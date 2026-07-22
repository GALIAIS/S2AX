package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	slowStreamingTTFTThreshold   = 5 * time.Second
	slowRequestDurationThreshold = 10 * time.Second
)

// logSlowGatewayTiming tail-samples successful requests without adding a
// synchronous database write to the forwarding path.
func logSlowGatewayTiming(ctx context.Context, gateway string, account *Account, model, requestID string, duration time.Duration, firstTokenMs *int) {
	if !isSlowGatewayTiming(duration, firstTokenMs) {
		return
	}
	durationMs := duration.Milliseconds()
	ttftMs := int64(0)
	if firstTokenMs != nil {
		ttftMs = int64(*firstTokenMs)
	}

	fields := []zap.Field{
		zap.String("gateway", strings.TrimSpace(gateway)),
		zap.String("model", strings.TrimSpace(model)),
		zap.String("request_id", strings.TrimSpace(requestID)),
		zap.Int64("duration_ms", durationMs),
		zap.Bool("has_first_token", firstTokenMs != nil),
	}
	if firstTokenMs != nil {
		fields = append(fields, zap.Int64("first_token_ms", ttftMs))
		if durationMs > ttftMs {
			fields = append(fields, zap.Int64("after_first_token_ms", durationMs-ttftMs))
		}
	}
	if account != nil {
		fields = append(fields,
			zap.Int64("account_id", account.ID),
			zap.String("platform", account.Platform),
			zap.String("account_type", string(account.Type)),
			zap.Bool("proxy_enabled", account.ProxyID != nil),
		)
	}
	if upstreamHeaderMs, ok := OpsLatencyMsFromContext(ctx, OpsUpstreamLatencyMsKey); ok {
		fields = append(fields, zap.Int64("upstream_header_ms", upstreamHeaderMs))
		if firstTokenMs != nil && upstreamHeaderMs <= ttftMs {
			fields = append(fields, zap.Int64("after_headers_to_first_token_ms", ttftMs-upstreamHeaderMs))
		}
	}
	for _, key := range []string{
		OpsAuthLatencyMsKey,
		OpsRoutingLatencyMsKey,
		OpsResponseLatencyMsKey,
		OpsOpenAIWSQueueWaitMsKey,
		OpsOpenAIWSConnPickMsKey,
	} {
		if value, ok := OpsLatencyMsFromContext(ctx, key); ok {
			fields = append(fields, zap.Int64(strings.TrimPrefix(key, "ops_"), value))
		}
	}
	logger.L().Warn("gateway.slow_timing", fields...)
}

func isSlowGatewayTiming(duration time.Duration, firstTokenMs *int) bool {
	if duration >= slowRequestDurationThreshold {
		return true
	}
	return firstTokenMs != nil && time.Duration(*firstTokenMs)*time.Millisecond >= slowStreamingTTFTThreshold
}
