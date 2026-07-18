package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type virtualCurrencyIntegrationNonceStore struct {
	client *redis.Client
}

func NewVirtualCurrencyIntegrationNonceStore(client *redis.Client) service.VirtualCurrencyIntegrationNonceStore {
	return &virtualCurrencyIntegrationNonceStore{client: client}
}

func (s *virtualCurrencyIntegrationNonceStore) Consume(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if s == nil || s.client == nil {
		return false, errors.New("virtual currency integration nonce store is not configured")
	}
	return s.client.SetNX(ctx, key, "1", ttl).Result()
}
