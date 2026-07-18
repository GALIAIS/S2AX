package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type virtualCurrencyIntegrationRepoStub struct {
	record *VirtualCurrencyIntegrationRecord
	scope  *VirtualCurrencyIntegrationScope
}

func (s *virtualCurrencyIntegrationRepoStub) List(context.Context, bool) ([]*VirtualCurrencyIntegrationRecord, error) {
	return []*VirtualCurrencyIntegrationRecord{s.record}, nil
}
func (s *virtualCurrencyIntegrationRepoStub) GetByID(context.Context, int64) (*VirtualCurrencyIntegrationRecord, error) {
	return s.record, nil
}
func (s *virtualCurrencyIntegrationRepoStub) GetByCode(context.Context, string) (*VirtualCurrencyIntegrationRecord, error) {
	if s.record == nil {
		return nil, ErrVirtualCurrencyIntegrationNotFound
	}
	return s.record, nil
}
func (s *virtualCurrencyIntegrationRepoStub) Create(context.Context, VirtualCurrencyIntegrationCreateRepositoryInput) (*VirtualCurrencyIntegrationRecord, error) {
	return s.record, nil
}
func (s *virtualCurrencyIntegrationRepoStub) Update(context.Context, int64, VirtualCurrencyIntegrationUpdateInput) (*VirtualCurrencyIntegrationRecord, error) {
	return s.record, nil
}
func (s *virtualCurrencyIntegrationRepoStub) SetStatus(context.Context, int64, string) (*VirtualCurrencyIntegrationRecord, error) {
	return s.record, nil
}
func (s *virtualCurrencyIntegrationRepoStub) RotateSecret(context.Context, VirtualCurrencyIntegrationRotateRepositoryInput) (*VirtualCurrencyIntegrationRecord, error) {
	return s.record, nil
}
func (s *virtualCurrencyIntegrationRepoStub) ListScopes(context.Context, int64) ([]*VirtualCurrencyIntegrationScope, error) {
	return []*VirtualCurrencyIntegrationScope{s.scope}, nil
}
func (s *virtualCurrencyIntegrationRepoStub) GetScope(context.Context, int64, int64, int64) (*VirtualCurrencyIntegrationScope, error) {
	if s.scope == nil {
		return nil, ErrVirtualCurrencyIntegrationNotFound
	}
	return s.scope, nil
}
func (s *virtualCurrencyIntegrationRepoStub) UpsertScope(context.Context, VirtualCurrencyIntegrationScopeInput) (*VirtualCurrencyIntegrationScope, error) {
	return s.scope, nil
}
func (s *virtualCurrencyIntegrationRepoStub) DeleteScope(context.Context, int64, int64, int64) error {
	return nil
}

type virtualCurrencyIntegrationEncryptorStub struct{}

func (virtualCurrencyIntegrationEncryptorStub) Encrypt(value string) (string, error) {
	return "enc:" + value, nil
}
func (virtualCurrencyIntegrationEncryptorStub) Decrypt(value string) (string, error) {
	if len(value) < 4 || value[:4] != "enc:" {
		return "", errors.New("invalid ciphertext")
	}
	return value[4:], nil
}

type virtualCurrencyIntegrationNonceStub struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (s *virtualCurrencyIntegrationNonceStub) Consume(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

func TestVirtualCurrencyIntegrationSignatureBindsBodyAndPath(t *testing.T) {
	body := []byte(`{"operation":"grant","user_id":42}`)
	first := VirtualCurrencyIntegrationSignature("POST", VirtualCurrencyIntegrationMutationPath, "100", "nonce-1234567890", body, "secret")
	second := VirtualCurrencyIntegrationSignature("POST", VirtualCurrencyIntegrationMutationPath, "100", "nonce-1234567890", []byte(`{"operation":"spend","user_id":42}`), "secret")
	require.Len(t, first, 64)
	require.NotEqual(t, first, second)
}

func TestVirtualCurrencyIntegrationAuthenticateDoesNotRevealUnknownCode(t *testing.T) {
	repo := &virtualCurrencyIntegrationRepoStub{}
	service := NewVirtualCurrencyIntegrationService(repo, virtualCurrencyIntegrationEncryptorStub{}, nil, &virtualCurrencyIntegrationNonceStub{})
	request := VirtualCurrencyIntegrationSignedRequest{
		Code:      "unknown-game",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
		Nonce:     "nonce-abcdefghijkl",
		Signature: "00",
		Method:    "POST",
		Path:      VirtualCurrencyIntegrationMutationPath,
		Body:      []byte(`{}`),
	}

	_, err := service.authenticate(context.Background(), request)

	require.ErrorIs(t, err, ErrVirtualCurrencyIntegrationSignature)
}

func TestVirtualCurrencyIntegrationAuthenticateRejectsTimestampOverflow(t *testing.T) {
	repo := &virtualCurrencyIntegrationRepoStub{record: &VirtualCurrencyIntegrationRecord{
		VirtualCurrencyIntegration: VirtualCurrencyIntegration{ID: 3, Code: "game-main", Status: VirtualCurrencyIntegrationStatusActive},
		SecretCiphertext:           "enc:integration-secret",
	}}
	service := NewVirtualCurrencyIntegrationService(repo, virtualCurrencyIntegrationEncryptorStub{}, nil, &virtualCurrencyIntegrationNonceStub{})
	_, err := service.authenticate(context.Background(), VirtualCurrencyIntegrationSignedRequest{
		Code: "game-main", Timestamp: strconv.FormatInt(-1<<63, 10), Nonce: "nonce-abcdefghijkl",
		Method: "POST", Path: VirtualCurrencyIntegrationMutationPath, Body: []byte(`{}`), Signature: "00",
	})
	require.ErrorIs(t, err, ErrVirtualCurrencyIntegrationSignature)
}

func TestVirtualCurrencyIntegrationAuthenticateRejectsWrongPath(t *testing.T) {
	service := NewVirtualCurrencyIntegrationService(&virtualCurrencyIntegrationRepoStub{}, virtualCurrencyIntegrationEncryptorStub{}, nil, &virtualCurrencyIntegrationNonceStub{})
	_, err := service.authenticate(context.Background(), VirtualCurrencyIntegrationSignedRequest{
		Code: "game-main", Timestamp: strconv.FormatInt(time.Now().Unix(), 10), Nonce: "nonce-abcdefghijkl",
		Method: "POST", Path: "/api/v1/integrations/other", Body: []byte(`{}`), Signature: "00",
	})
	require.ErrorIs(t, err, ErrVirtualCurrencyIntegrationSignature)
}

func TestVirtualCurrencyIntegrationExecuteRejectsReusedNonce(t *testing.T) {
	repo := &virtualCurrencyIntegrationRepoStub{record: &VirtualCurrencyIntegrationRecord{
		VirtualCurrencyIntegration: VirtualCurrencyIntegration{ID: 3, Code: "game-main", Status: VirtualCurrencyIntegrationStatusActive},
		SecretCiphertext:           "enc:integration-secret",
	}}
	service := NewVirtualCurrencyIntegrationService(repo, virtualCurrencyIntegrationEncryptorStub{}, NewVirtualCurrencyService(&virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive}}), &virtualCurrencyIntegrationNonceStub{})
	body := []byte(`{"operation":"unknown","user_id":42,"source_id":"event-1","idempotency_key":"x"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request := VirtualCurrencyIntegrationSignedRequest{
		Code: "game-main", Timestamp: timestamp, Nonce: "nonce-1234567890", Method: "POST",
		Path: VirtualCurrencyIntegrationMutationPath, Body: body,
		Mutation: VirtualCurrencyIntegrationMutation{Operation: "unknown", UserID: 42, SourceID: "event-1", IdempotencyKey: "x"},
	}
	request.Signature = VirtualCurrencyIntegrationSignature(request.Method, request.Path, request.Timestamp, request.Nonce, body, "integration-secret")
	_, err := service.ExecuteSigned(context.Background(), request)
	require.ErrorIs(t, err, ErrVirtualCurrencyIntegrationInvalid)
	_, err = service.ExecuteSigned(context.Background(), request)
	require.ErrorIs(t, err, ErrVirtualCurrencyIntegrationReplay)
}

func TestVirtualCurrencyIntegrationExecuteRequiresStableSourceID(t *testing.T) {
	repo := &virtualCurrencyIntegrationRepoStub{record: &VirtualCurrencyIntegrationRecord{
		VirtualCurrencyIntegration: VirtualCurrencyIntegration{ID: 3, Code: "game-main", Status: VirtualCurrencyIntegrationStatusActive},
		SecretCiphertext:           "enc:integration-secret",
	}}
	service := NewVirtualCurrencyIntegrationService(repo, virtualCurrencyIntegrationEncryptorStub{}, NewVirtualCurrencyService(&virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive}}), &virtualCurrencyIntegrationNonceStub{})
	body := []byte(`{"operation":"grant","currency_code":"gold","user_id":42,"group_id":9,"amount_units":25,"idempotency_key":"reward-1"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request := VirtualCurrencyIntegrationSignedRequest{
		Code: "game-main", Timestamp: timestamp, Nonce: "nonce-source-missing", Method: "POST",
		Path: VirtualCurrencyIntegrationMutationPath, Body: body,
		Mutation: VirtualCurrencyIntegrationMutation{Operation: "grant", CurrencyCode: "gold", UserID: 42, GroupID: 9, AmountUnits: 25, IdempotencyKey: "reward-1"},
	}
	request.Signature = VirtualCurrencyIntegrationSignature(request.Method, request.Path, request.Timestamp, request.Nonce, body, "integration-secret")
	_, err := service.ExecuteSigned(context.Background(), request)
	require.ErrorIs(t, err, ErrVirtualCurrencyIntegrationInvalid)
}

func TestVirtualCurrencyIntegrationIdempotencyNamespaceIsOperationIndependent(t *testing.T) {
	repo := &virtualCurrencyIntegrationRepoStub{
		record: &VirtualCurrencyIntegrationRecord{
			VirtualCurrencyIntegration: VirtualCurrencyIntegration{ID: 3, Code: "game-main", Status: VirtualCurrencyIntegrationStatusActive},
			SecretCiphertext:           "enc:integration-secret",
		},
		scope: &VirtualCurrencyIntegrationScope{Enabled: true, CanEarn: true, CanSpend: true},
	}
	currencyRepo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive}}
	service := NewVirtualCurrencyIntegrationService(repo, virtualCurrencyIntegrationEncryptorStub{}, NewVirtualCurrencyService(currencyRepo), &virtualCurrencyIntegrationNonceStub{})

	grantBody := []byte(`{"operation":"grant","currency_code":"gold","user_id":42,"group_id":9,"amount_units":25,"source_id":"reward-event","idempotency_key":"shared-key"}`)
	grantRequest := VirtualCurrencyIntegrationSignedRequest{
		Code: "game-main", Timestamp: strconv.FormatInt(time.Now().Unix(), 10), Nonce: "nonce-idem-grant", Method: "POST",
		Path: VirtualCurrencyIntegrationMutationPath, Body: grantBody,
		Mutation: VirtualCurrencyIntegrationMutation{Operation: "grant", CurrencyCode: "gold", UserID: 42, GroupID: 9, AmountUnits: 25, SourceID: "reward-event", IdempotencyKey: "shared-key"},
	}
	grantRequest.Signature = VirtualCurrencyIntegrationSignature(grantRequest.Method, grantRequest.Path, grantRequest.Timestamp, grantRequest.Nonce, grantBody, "integration-secret")
	_, err := service.ExecuteSigned(context.Background(), grantRequest)
	require.NoError(t, err)
	grantKey := currencyRepo.delta.IdempotencyKey

	spendBody := []byte(`{"operation":"spend","currency_code":"gold","user_id":42,"group_id":9,"amount_units":1,"source_id":"reward-event","idempotency_key":"shared-key"}`)
	spendRequest := VirtualCurrencyIntegrationSignedRequest{
		Code: "game-main", Timestamp: strconv.FormatInt(time.Now().Unix(), 10), Nonce: "nonce-idem-spend", Method: "POST",
		Path: VirtualCurrencyIntegrationMutationPath, Body: spendBody,
		Mutation: VirtualCurrencyIntegrationMutation{Operation: "spend", CurrencyCode: "gold", UserID: 42, GroupID: 9, AmountUnits: 1, SourceID: "reward-event", IdempotencyKey: "shared-key"},
	}
	spendRequest.Signature = VirtualCurrencyIntegrationSignature(spendRequest.Method, spendRequest.Path, spendRequest.Timestamp, spendRequest.Nonce, spendBody, "integration-secret")
	_, err = service.ExecuteSigned(context.Background(), spendRequest)
	require.NoError(t, err)
	require.Equal(t, grantKey, currencyRepo.delta.IdempotencyKey, "the repository must see one integration-wide idempotency namespace")
}

func TestVirtualCurrencyIntegrationGrantUsesScopedIntegrationSource(t *testing.T) {
	repo := &virtualCurrencyIntegrationRepoStub{
		record: &VirtualCurrencyIntegrationRecord{
			VirtualCurrencyIntegration: VirtualCurrencyIntegration{ID: 3, Code: "game-main", Status: VirtualCurrencyIntegrationStatusActive},
			SecretCiphertext:           "enc:integration-secret",
		},
		scope: &VirtualCurrencyIntegrationScope{Enabled: true, CanEarn: true},
	}
	currencyRepo := &virtualCurrencyRepositoryStub{currency: &VirtualCurrency{ID: 7, Code: "gold", Status: VirtualCurrencyStatusActive}}
	service := NewVirtualCurrencyIntegrationService(repo, virtualCurrencyIntegrationEncryptorStub{}, NewVirtualCurrencyService(currencyRepo), &virtualCurrencyIntegrationNonceStub{})
	body := []byte(`{"operation":"grant","currency_code":"gold","user_id":42,"group_id":9,"amount_units":25,"source_id":"event-1","idempotency_key":"reward-1"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request := VirtualCurrencyIntegrationSignedRequest{
		Code: "game-main", Timestamp: timestamp, Nonce: "nonce-abcdefghijkl", Method: "POST",
		Path: VirtualCurrencyIntegrationMutationPath, Body: body,
		Mutation: VirtualCurrencyIntegrationMutation{Operation: "grant", CurrencyCode: "gold", UserID: 42, GroupID: 9, AmountUnits: 25, SourceID: "event-1", IdempotencyKey: "reward-1"},
	}
	request.Signature = VirtualCurrencyIntegrationSignature(request.Method, request.Path, request.Timestamp, request.Nonce, body, "integration-secret")
	result, err := service.ExecuteSigned(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result.Ledger)
	require.Equal(t, VirtualCurrencySourceIntegration, currencyRepo.delta.SourceType)
	require.True(t, currencyRepo.delta.RequireUserAccess)
	require.Contains(t, currencyRepo.delta.IdempotencyKey, "integration:3:")
}
