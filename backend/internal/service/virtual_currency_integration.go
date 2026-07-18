package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	VirtualCurrencyIntegrationStatusActive    = "active"
	VirtualCurrencyIntegrationStatusDisabled  = "disabled"
	virtualCurrencyIntegrationSignatureWindow = 5 * time.Minute
	virtualCurrencyIntegrationNonceTTL        = 10 * time.Minute
	maxVirtualCurrencyIntegrationBodyBytes    = 1 << 20
	VirtualCurrencyIntegrationMutationPath    = "/api/v1/integrations/virtual-currency/mutations"
)

var (
	ErrVirtualCurrencyIntegrationNotFound    = infraerrors.NotFound("VIRTUAL_CURRENCY_INTEGRATION_NOT_FOUND", "virtual currency integration not found")
	ErrVirtualCurrencyIntegrationCodeExists  = infraerrors.Conflict("VIRTUAL_CURRENCY_INTEGRATION_CODE_EXISTS", "virtual currency integration code already exists")
	ErrVirtualCurrencyIntegrationDisabled    = infraerrors.Unauthorized("VIRTUAL_CURRENCY_INTEGRATION_DISABLED", "virtual currency integration is disabled")
	ErrVirtualCurrencyIntegrationInvalid     = infraerrors.BadRequest("VIRTUAL_CURRENCY_INTEGRATION_INVALID", "invalid virtual currency integration request")
	ErrVirtualCurrencyIntegrationSignature   = infraerrors.Unauthorized("VIRTUAL_CURRENCY_INTEGRATION_SIGNATURE_INVALID", "invalid virtual currency integration signature")
	ErrVirtualCurrencyIntegrationReplay      = infraerrors.Conflict("VIRTUAL_CURRENCY_INTEGRATION_REPLAY", "virtual currency integration request has already been used")
	ErrVirtualCurrencyIntegrationScopeDenied = infraerrors.Forbidden("VIRTUAL_CURRENCY_INTEGRATION_SCOPE_DENIED", "virtual currency integration is not allowed for this currency and group")
	ErrVirtualCurrencyIntegrationUnavailable = infraerrors.ServiceUnavailable("VIRTUAL_CURRENCY_INTEGRATION_UNAVAILABLE", "virtual currency integration security service unavailable")
)

var virtualCurrencyIntegrationCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,63}$`)
var virtualCurrencyIntegrationNoncePattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,128}$`)

type VirtualCurrencyIntegration struct {
	ID         int64
	Code       string
	Name       string
	SecretHint string
	Status     string
	Metadata   map[string]any
	CreatedBy  *int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type VirtualCurrencyIntegrationRecord struct {
	VirtualCurrencyIntegration
	SecretCiphertext string
}

type VirtualCurrencyIntegrationScope struct {
	ID            int64
	IntegrationID int64
	CurrencyID    int64
	GroupID       int64
	Enabled       bool
	CanEarn       bool
	CanSpend      bool
	CanSettle     bool
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type VirtualCurrencyIntegrationCreateInput struct {
	Code      string
	Name      string
	Metadata  map[string]any
	CreatedBy *int64
}

type VirtualCurrencyIntegrationUpdateInput struct {
	Name     *string
	Metadata map[string]any
}

type VirtualCurrencyIntegrationScopeInput struct {
	IntegrationID int64
	CurrencyID    int64
	GroupID       int64
	Enabled       bool
	CanEarn       bool
	CanSpend      bool
	CanSettle     bool
	Metadata      map[string]any
}

type VirtualCurrencyIntegrationCreateRepositoryInput struct {
	Code             string
	Name             string
	SecretCiphertext string
	SecretHint       string
	Metadata         map[string]any
	CreatedBy        *int64
}

type VirtualCurrencyIntegrationRotateRepositoryInput struct {
	ID               int64
	SecretCiphertext string
	SecretHint       string
}

type VirtualCurrencyIntegrationSecretResult struct {
	Integration *VirtualCurrencyIntegration
	Secret      string
}

type VirtualCurrencyIntegrationRepository interface {
	List(ctx context.Context, includeDisabled bool) ([]*VirtualCurrencyIntegrationRecord, error)
	GetByID(ctx context.Context, id int64) (*VirtualCurrencyIntegrationRecord, error)
	GetByCode(ctx context.Context, code string) (*VirtualCurrencyIntegrationRecord, error)
	Create(ctx context.Context, input VirtualCurrencyIntegrationCreateRepositoryInput) (*VirtualCurrencyIntegrationRecord, error)
	Update(ctx context.Context, id int64, input VirtualCurrencyIntegrationUpdateInput) (*VirtualCurrencyIntegrationRecord, error)
	SetStatus(ctx context.Context, id int64, status string) (*VirtualCurrencyIntegrationRecord, error)
	RotateSecret(ctx context.Context, input VirtualCurrencyIntegrationRotateRepositoryInput) (*VirtualCurrencyIntegrationRecord, error)
	ListScopes(ctx context.Context, integrationID int64) ([]*VirtualCurrencyIntegrationScope, error)
	GetScope(ctx context.Context, integrationID, currencyID, groupID int64) (*VirtualCurrencyIntegrationScope, error)
	UpsertScope(ctx context.Context, input VirtualCurrencyIntegrationScopeInput) (*VirtualCurrencyIntegrationScope, error)
	DeleteScope(ctx context.Context, integrationID, currencyID, groupID int64) error
}

type VirtualCurrencyIntegrationNonceStore interface {
	Consume(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

type VirtualCurrencyIntegrationMutation struct {
	Operation      string         `json:"operation"`
	CurrencyCode   string         `json:"currency_code"`
	UserID         int64          `json:"user_id"`
	GroupID        int64          `json:"group_id"`
	AmountUnits    int64          `json:"amount_units"`
	HoldID         int64          `json:"hold_id"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	SourceID       string         `json:"source_id"`
	IdempotencyKey string         `json:"idempotency_key"`
	Reason         string         `json:"reason"`
	Metadata       map[string]any `json:"metadata"`
}

type VirtualCurrencyIntegrationSignedRequest struct {
	Code      string
	Timestamp string
	Nonce     string
	Signature string
	Method    string
	Path      string
	Body      []byte
	Mutation  VirtualCurrencyIntegrationMutation
}

type VirtualCurrencyIntegrationMutationResult struct {
	Operation string
	Ledger    *VirtualCurrencyLedgerEntry
	Hold      *VirtualCurrencyHold
}

type VirtualCurrencyIntegrationService struct {
	repo       VirtualCurrencyIntegrationRepository
	encryptor  SecretEncryptor
	currency   *VirtualCurrencyService
	nonceStore VirtualCurrencyIntegrationNonceStore
}

func NewVirtualCurrencyIntegrationService(repo VirtualCurrencyIntegrationRepository, encryptor SecretEncryptor, currency *VirtualCurrencyService, nonceStore VirtualCurrencyIntegrationNonceStore) *VirtualCurrencyIntegrationService {
	return &VirtualCurrencyIntegrationService{repo: repo, encryptor: encryptor, currency: currency, nonceStore: nonceStore}
}

func (s *VirtualCurrencyIntegrationService) List(ctx context.Context, includeDisabled bool) ([]*VirtualCurrencyIntegration, error) {
	records, err := s.repo.List(ctx, includeDisabled)
	if err != nil {
		return nil, err
	}
	out := make([]*VirtualCurrencyIntegration, 0, len(records))
	for _, record := range records {
		if record != nil {
			out = append(out, &record.VirtualCurrencyIntegration)
		}
	}
	return out, nil
}

func (s *VirtualCurrencyIntegrationService) Get(ctx context.Context, id int64) (*VirtualCurrencyIntegration, error) {
	if id <= 0 {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	record, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &record.VirtualCurrencyIntegration, nil
}

func (s *VirtualCurrencyIntegrationService) Create(ctx context.Context, input VirtualCurrencyIntegrationCreateInput) (*VirtualCurrencyIntegrationSecretResult, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	if !virtualCurrencyIntegrationCodePattern.MatchString(input.Code) || input.Name == "" || len([]rune(input.Name)) > 128 {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	secret, err := generateVirtualCurrencyIntegrationSecret()
	if err != nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}
	ciphertext, err := s.encryptSecret(secret)
	if err != nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}
	record, err := s.repo.Create(ctx, VirtualCurrencyIntegrationCreateRepositoryInput{
		Code: input.Code, Name: input.Name, SecretCiphertext: ciphertext,
		SecretHint: virtualCurrencyIntegrationSecretHint(secret), Metadata: input.Metadata, CreatedBy: input.CreatedBy,
	})
	if err != nil {
		return nil, err
	}
	return &VirtualCurrencyIntegrationSecretResult{Integration: &record.VirtualCurrencyIntegration, Secret: secret}, nil
}

func (s *VirtualCurrencyIntegrationService) Update(ctx context.Context, id int64, input VirtualCurrencyIntegrationUpdateInput) (*VirtualCurrencyIntegration, error) {
	if id <= 0 || (input.Name == nil && input.Metadata == nil) {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > 128 {
			return nil, ErrVirtualCurrencyIntegrationInvalid
		}
		input.Name = &name
	}
	if input.Metadata != nil {
		if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
			return nil, err
		}
	}
	record, err := s.repo.Update(ctx, id, input)
	if err != nil {
		return nil, err
	}
	return &record.VirtualCurrencyIntegration, nil
}

func (s *VirtualCurrencyIntegrationService) SetStatus(ctx context.Context, id int64, status string) (*VirtualCurrencyIntegration, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if id <= 0 || (status != VirtualCurrencyIntegrationStatusActive && status != VirtualCurrencyIntegrationStatusDisabled) {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	record, err := s.repo.SetStatus(ctx, id, status)
	if err != nil {
		return nil, err
	}
	return &record.VirtualCurrencyIntegration, nil
}

func (s *VirtualCurrencyIntegrationService) RotateSecret(ctx context.Context, id int64) (*VirtualCurrencyIntegrationSecretResult, error) {
	if id <= 0 {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	secret, err := generateVirtualCurrencyIntegrationSecret()
	if err != nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}
	ciphertext, err := s.encryptSecret(secret)
	if err != nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}
	record, err := s.repo.RotateSecret(ctx, VirtualCurrencyIntegrationRotateRepositoryInput{
		ID: id, SecretCiphertext: ciphertext, SecretHint: virtualCurrencyIntegrationSecretHint(secret),
	})
	if err != nil {
		return nil, err
	}
	return &VirtualCurrencyIntegrationSecretResult{Integration: &record.VirtualCurrencyIntegration, Secret: secret}, nil
}

func (s *VirtualCurrencyIntegrationService) ListScopes(ctx context.Context, integrationID int64) ([]*VirtualCurrencyIntegrationScope, error) {
	if integrationID <= 0 {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	return s.repo.ListScopes(ctx, integrationID)
}

func (s *VirtualCurrencyIntegrationService) UpsertScope(ctx context.Context, input VirtualCurrencyIntegrationScopeInput) (*VirtualCurrencyIntegrationScope, error) {
	if input.IntegrationID <= 0 || input.CurrencyID <= 0 || input.GroupID <= 0 {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(input.Metadata); err != nil {
		return nil, err
	}
	return s.repo.UpsertScope(ctx, input)
}

func (s *VirtualCurrencyIntegrationService) DeleteScope(ctx context.Context, integrationID, currencyID, groupID int64) error {
	if integrationID <= 0 || currencyID <= 0 || groupID <= 0 {
		return ErrVirtualCurrencyIntegrationInvalid
	}
	return s.repo.DeleteScope(ctx, integrationID, currencyID, groupID)
}

func (s *VirtualCurrencyIntegrationService) ExecuteSigned(ctx context.Context, request VirtualCurrencyIntegrationSignedRequest) (*VirtualCurrencyIntegrationMutationResult, error) {
	record, err := s.authenticate(ctx, request)
	if err != nil {
		return nil, err
	}
	mutation := request.Mutation
	mutation.Operation = strings.ToLower(strings.TrimSpace(mutation.Operation))
	mutation.CurrencyCode = strings.ToLower(strings.TrimSpace(mutation.CurrencyCode))
	mutation.SourceID = strings.TrimSpace(mutation.SourceID)
	mutation.IdempotencyKey = strings.TrimSpace(mutation.IdempotencyKey)
	mutation.Reason = strings.TrimSpace(mutation.Reason)
	if len(request.Body) > maxVirtualCurrencyIntegrationBodyBytes || mutation.UserID <= 0 || mutation.IdempotencyKey == "" || mutation.SourceID == "" || len([]rune(mutation.IdempotencyKey)) > 64 || len([]rune(mutation.SourceID)) > maxVirtualCurrencySourceIDRunes || len([]rune(mutation.Reason)) > maxVirtualCurrencyReasonRunes {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	if mutation.Metadata == nil {
		mutation.Metadata = map[string]any{}
	}
	if err := validateVirtualCurrencyMetadata(mutation.Metadata); err != nil {
		return nil, err
	}
	if mutation.GroupID < 0 || mutation.AmountUnits < 0 || mutation.HoldID < 0 {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	if s.currency == nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}

	// The idempotency key covers the complete signed mutation, including its
	// operation. Reusing it for a different operation must conflict instead of
	// creating a second ledger entry in a separate operation namespace.
	keyPrefix := fmt.Sprintf("integration:%d:", record.ID)
	idempotencyKey := keyPrefix + mutation.IdempotencyKey
	if len([]rune(idempotencyKey)) > maxVirtualCurrencySourceIDRunes {
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	result := &VirtualCurrencyIntegrationMutationResult{Operation: mutation.Operation}
	switch mutation.Operation {
	case "grant":
		currency, resolveErr := s.currency.GetCurrencyByCode(ctx, mutation.CurrencyCode)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if scopeErr := s.requireScope(ctx, record.ID, currency.ID, mutation.GroupID, func(scope *VirtualCurrencyIntegrationScope) bool { return scope.CanEarn }); scopeErr != nil {
			return nil, scopeErr
		}
		if mutation.AmountUnits <= 0 {
			return nil, ErrVirtualCurrencyIntegrationInvalid
		}
		ledger, grantErr := s.currency.Grant(ctx, VirtualCurrencyGrantInput{
			CurrencyCode: mutation.CurrencyCode, UserID: mutation.UserID, GroupID: mutation.GroupID,
			AmountUnits: mutation.AmountUnits, SourceType: VirtualCurrencySourceIntegration,
			SourceID: mutation.SourceID, IdempotencyKey: idempotencyKey, Reason: mutation.Reason,
			Metadata: mutation.Metadata, RequireUserAccess: true,
		})
		if grantErr != nil {
			return nil, grantErr
		}
		result.Ledger = ledger
	case "spend":
		currency, resolveErr := s.currency.GetCurrencyByCode(ctx, mutation.CurrencyCode)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if scopeErr := s.requireScope(ctx, record.ID, currency.ID, mutation.GroupID, func(scope *VirtualCurrencyIntegrationScope) bool { return scope.CanSpend }); scopeErr != nil {
			return nil, scopeErr
		}
		ledger, spendErr := s.currency.Spend(ctx, VirtualCurrencySpendInput{
			CurrencyCode: mutation.CurrencyCode, UserID: mutation.UserID, GroupID: mutation.GroupID,
			AmountUnits: mutation.AmountUnits, SourceType: VirtualCurrencySourceIntegration,
			SourceID: mutation.SourceID, IdempotencyKey: idempotencyKey, Reason: mutation.Reason,
			Metadata: mutation.Metadata,
		})
		if spendErr != nil {
			return nil, spendErr
		}
		result.Ledger = ledger
	case "reserve":
		currency, resolveErr := s.currency.GetCurrencyByCode(ctx, mutation.CurrencyCode)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if scopeErr := s.requireScope(ctx, record.ID, currency.ID, mutation.GroupID, func(scope *VirtualCurrencyIntegrationScope) bool { return scope.CanSpend }); scopeErr != nil {
			return nil, scopeErr
		}
		holdResult, reserveErr := s.currency.ReserveHold(ctx, VirtualCurrencyReserveInput{
			CurrencyCode: mutation.CurrencyCode, UserID: mutation.UserID, GroupID: mutation.GroupID,
			AmountUnits: mutation.AmountUnits, ExpiresAt: timeValue(mutation.ExpiresAt), SourceType: VirtualCurrencySourceIntegration,
			SourceID: mutation.SourceID, IdempotencyKey: idempotencyKey, Reason: mutation.Reason, Metadata: mutation.Metadata,
		})
		if reserveErr != nil {
			return nil, reserveErr
		}
		result.Hold, result.Ledger = holdResult.Hold, holdResult.Ledger
	case "commit", "release":
		if mutation.HoldID <= 0 {
			return nil, ErrVirtualCurrencyIntegrationInvalid
		}
		hold, holdErr := s.currency.GetHold(ctx, mutation.UserID, mutation.HoldID)
		if holdErr != nil {
			return nil, holdErr
		}
		if scopeErr := s.requireScope(ctx, record.ID, hold.CurrencyID, dereferenceInt64(hold.GroupID), func(scope *VirtualCurrencyIntegrationScope) bool { return scope.CanSettle }); scopeErr != nil {
			return nil, scopeErr
		}
		settlementInput := VirtualCurrencyHoldSettlementInput{
			HoldID: mutation.HoldID, UserID: mutation.UserID, SourceType: VirtualCurrencySourceIntegration,
			SourceID: mutation.SourceID, IdempotencyKey: idempotencyKey, Reason: mutation.Reason, Metadata: mutation.Metadata,
		}
		var holdResult *VirtualCurrencyHoldResult
		if mutation.Operation == "commit" {
			holdResult, err = s.currency.CommitHold(ctx, settlementInput)
		} else {
			holdResult, err = s.currency.ReleaseHold(ctx, settlementInput)
		}
		if err != nil {
			return nil, err
		}
		result.Hold, result.Ledger = holdResult.Hold, holdResult.Ledger
	default:
		return nil, ErrVirtualCurrencyIntegrationInvalid
	}
	return result, nil
}

func (s *VirtualCurrencyIntegrationService) authenticate(ctx context.Context, request VirtualCurrencyIntegrationSignedRequest) (*VirtualCurrencyIntegrationRecord, error) {
	code := strings.ToLower(strings.TrimSpace(request.Code))
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	path := strings.TrimSpace(request.Path)
	if !virtualCurrencyIntegrationCodePattern.MatchString(code) || method != "POST" || path != VirtualCurrencyIntegrationMutationPath || len(request.Body) > maxVirtualCurrencyIntegrationBodyBytes || !virtualCurrencyIntegrationNoncePattern.MatchString(strings.TrimSpace(request.Nonce)) {
		return nil, ErrVirtualCurrencyIntegrationSignature
	}
	timestamp, err := strconv.ParseInt(strings.TrimSpace(request.Timestamp), 10, 64)
	now := time.Now().Unix()
	window := int64(virtualCurrencyIntegrationSignatureWindow / time.Second)
	if err != nil || timestamp < now-window || timestamp > now+window {
		return nil, ErrVirtualCurrencyIntegrationSignature
	}
	record, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		// Do not reveal whether an integration code exists. Treat lookup
		// failures at this authentication boundary as a generic signature
		// failure; operational database errors are still handled by the
		// repository layer and surfaced through the service availability path
		// only where the secret/nonce checks require it.
		if errors.Is(err, ErrVirtualCurrencyIntegrationNotFound) {
			return nil, ErrVirtualCurrencyIntegrationSignature
		}
		return nil, err
	}
	if record.Status != VirtualCurrencyIntegrationStatusActive {
		return nil, ErrVirtualCurrencyIntegrationDisabled
	}
	secret, err := s.decryptSecret(record.SecretCiphertext)
	if err != nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}
	provided := strings.TrimSpace(request.Signature)
	provided = strings.TrimPrefix(provided, "sha256=")
	providedBytes, err := hex.DecodeString(provided)
	if err != nil || len(providedBytes) != sha256.Size {
		return nil, ErrVirtualCurrencyIntegrationSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(integrationCanonicalPayload(method, path, request.Timestamp, request.Nonce, request.Body)))
	if !hmac.Equal(providedBytes, mac.Sum(nil)) {
		return nil, ErrVirtualCurrencyIntegrationSignature
	}
	if s.nonceStore == nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}
	consumed, err := s.nonceStore.Consume(ctx, fmt.Sprintf("virtual-currency:integration:%d:nonce:%s", record.ID, request.Nonce), virtualCurrencyIntegrationNonceTTL)
	if err != nil {
		return nil, ErrVirtualCurrencyIntegrationUnavailable
	}
	if !consumed {
		return nil, ErrVirtualCurrencyIntegrationReplay
	}
	return record, nil
}

func (s *VirtualCurrencyIntegrationService) requireScope(ctx context.Context, integrationID, currencyID, groupID int64, allowed func(*VirtualCurrencyIntegrationScope) bool) error {
	if integrationID <= 0 || currencyID <= 0 || groupID <= 0 {
		return ErrVirtualCurrencyIntegrationScopeDenied
	}
	scope, err := s.repo.GetScope(ctx, integrationID, currencyID, groupID)
	if errors.Is(err, ErrVirtualCurrencyIntegrationNotFound) {
		return ErrVirtualCurrencyIntegrationScopeDenied
	}
	if err != nil {
		return err
	}
	if scope == nil || !scope.Enabled || allowed == nil || !allowed(scope) {
		return ErrVirtualCurrencyIntegrationScopeDenied
	}
	return nil
}

func (s *VirtualCurrencyIntegrationService) encryptSecret(secret string) (string, error) {
	if s.encryptor == nil {
		return "", ErrVirtualCurrencyIntegrationUnavailable
	}
	return s.encryptor.Encrypt(secret)
}

func (s *VirtualCurrencyIntegrationService) decryptSecret(ciphertext string) (string, error) {
	if s.encryptor == nil {
		return "", ErrVirtualCurrencyIntegrationUnavailable
	}
	return s.encryptor.Decrypt(ciphertext)
}

func generateVirtualCurrencyIntegrationSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func virtualCurrencyIntegrationSecretHint(secret string) string {
	if len(secret) <= 8 {
		return secret
	}
	return secret[len(secret)-8:]
}

func integrationCanonicalPayload(method, path, timestamp, nonce string, body []byte) string {
	return strings.ToUpper(strings.TrimSpace(method)) + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + string(body)
}

func VirtualCurrencyIntegrationSignature(method, path, timestamp, nonce string, body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(integrationCanonicalPayload(method, path, timestamp, nonce, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
