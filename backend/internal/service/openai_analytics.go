package service

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	chatGPTDailyWorkspaceUsageURL = "https://chatgpt.com/backend-api/wham/analytics/daily-workspace-usage-counts"
	// OpenAI's current Codex credit example is 1,000 credits for $40. Keep the
	// conversion on every snapshot so a future rate-card change is auditable.
	openAIAnalyticsCreditsPerUSD = 25.0
)

// OpenAIAnalyticsUsage is the normalized, aggregate projection of the private
// daily workspace usage endpoint. It deliberately contains no account token or
// raw response body.
type OpenAIAnalyticsUsage struct {
	Credits           float64   `json:"credits"`
	CreditsAvailable  bool      `json:"credits_available"`
	InputTokens       int64     `json:"input_tokens"`
	CachedInputTokens int64     `json:"cached_input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	TotalTokens       int64     `json:"total_tokens"`
	StartDate         time.Time `json:"start_date"`
	EndDate           time.Time `json:"end_date"`
	FetchedAt         time.Time `json:"fetched_at"`
	Source            string    `json:"source"`
	Status            string    `json:"status"`
	Confidence        float64   `json:"confidence"`
	RecordCount       int       `json:"record_count"`
	CreditsPerUSD     float64   `json:"credits_per_usd"`
}

// QueryAnalytics fetches the daily credit aggregate used to calibrate an
// official rolling quota window. It is called by the background shared-pool
// refresh, never by the gateway request path.
func (s *OpenAIQuotaService) QueryAnalytics(ctx context.Context, accountID int64, startDate, endDate time.Time) (*OpenAIAnalyticsUsage, error) {
	if endDate.Before(startDate) {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_ANALYTICS_INVALID_RANGE", "analytics end date precedes start date")
	}
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_ANALYTICS_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	agentIdentity := s.isAgentIdentityAccount(ctx, accountID)
	for recovered := false; ; {
		headers, expectedTaskID, headerErr := s.buildCodexQuotaHeaders(callCtx, accountID, accessToken, chatGPTAccountID, fedRAMP)
		if headerErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_ANALYTICS_AUTH_FAILED", "failed to build upstream authentication: %v", headerErr)
		}
		resp, requestErr := client.R().
			SetContext(callCtx).
			SetHeaders(headers).
			SetQueryParam("start_date", startDate.UTC().Format("2006-01-02")).
			SetQueryParam("end_date", endDate.UTC().Format("2006-01-02")).
			SetQueryParam("group_by", "day").
			Get(chatGPTDailyWorkspaceUsageURL)
		if requestErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_ANALYTICS_REQUEST_FAILED", "upstream request failed: %v", requestErr)
		}
		if !resp.IsSuccessState() {
			if agentIdentity && !recovered && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, []byte(resp.String())) {
				recovered = true
				if recoverErr := s.recoverAgentIdentityTask(ctx, accountID, expectedTaskID); recoverErr != nil {
					return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_ANALYTICS_AUTH_FAILED", "agent identity task recovery failed: %v", recoverErr)
				}
				continue
			}
			body := truncate(s.redactQuotaErrorBody(ctx, accountID, resp.String()), 240)
			return nil, infraerrors.Newf(mapUpstreamStatus(resp.StatusCode), "OPENAI_ANALYTICS_UPSTREAM_ERROR", "upstream returned %d: %s", resp.StatusCode, body)
		}
		usage, parseErr := parseOpenAIAnalyticsUsage(resp.Bytes(), startDate, endDate)
		if parseErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_ANALYTICS_PARSE_FAILED", "failed to parse analytics response: %v", parseErr)
		}
		usage.FetchedAt = time.Now().UTC()
		return usage, nil
	}
}

var (
	analyticsCreditKeys = map[string]struct{}{
		"credit": {}, "credits": {}, "totalcredit": {}, "totalcredits": {},
		"creditusage": {}, "creditused": {}, "usedcredit": {}, "usedcredits": {},
		"creditsused": {}, "consumedcredits": {}, "usagecredits": {}, "totalusagecredits": {},
	}
	analyticsInputKeys = map[string]struct{}{
		"inputtoken": {}, "inputtokens": {}, "prompttoken": {}, "prompttokens": {},
	}
	analyticsCachedInputKeys = map[string]struct{}{
		"cachedinputtoken": {}, "cachedinputtokens": {}, "cacheinputtoken": {},
		"cacheinputtokens": {}, "cachereadinputtoken": {}, "cachereadinputtokens": {},
	}
	analyticsOutputKeys = map[string]struct{}{
		"outputtoken": {}, "outputtokens": {}, "completiontoken": {}, "completiontokens": {},
	}
	analyticsTotalTokenKeys = map[string]struct{}{
		"totaltoken": {}, "totaltokens": {}, "token": {}, "tokens": {},
	}
)

func parseOpenAIAnalyticsUsage(body []byte, startDate, endDate time.Time) (*OpenAIAnalyticsUsage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	rows := analyticsRows(root)
	usage := &OpenAIAnalyticsUsage{
		StartDate:     startDate.UTC(),
		EndDate:       endDate.UTC(),
		Source:        "chatgpt_wham_analytics",
		CreditsPerUSD: openAIAnalyticsCreditsPerUSD,
		RecordCount:   len(rows),
	}
	if len(rows) == 0 {
		usage.Status = "no_data"
		return usage, nil
	}

	var inputFound, cachedFound, outputFound, totalFound bool
	for _, row := range rows {
		if value, ok := analyticsNumber(row, analyticsCreditKeys); ok {
			usage.Credits += math.Max(value, 0)
			usage.CreditsAvailable = true
		}
		if value, ok := analyticsNumber(row, analyticsInputKeys); ok {
			usage.InputTokens += nonNegativeInt64(value)
			inputFound = true
		}
		if value, ok := analyticsNumber(row, analyticsCachedInputKeys); ok {
			usage.CachedInputTokens += nonNegativeInt64(value)
			cachedFound = true
		}
		if value, ok := analyticsNumber(row, analyticsOutputKeys); ok {
			usage.OutputTokens += nonNegativeInt64(value)
			outputFound = true
		}
		if value, ok := analyticsNumber(row, analyticsTotalTokenKeys); ok {
			usage.TotalTokens += nonNegativeInt64(value)
			totalFound = true
		}
	}
	if !inputFound && !cachedFound && !outputFound && !totalFound && !usage.CreditsAvailable {
		usage.Status = "unsupported_shape"
		return usage, nil
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.CachedInputTokens + usage.OutputTokens
	}
	if usage.CreditsAvailable {
		usage.Status = "available"
		usage.Confidence = 1
	} else {
		usage.Status = "tokens_only"
		usage.Confidence = 0.5
	}
	return usage, nil
}

func analyticsRows(root any) []map[string]any {
	if rows := analyticsRowsFromArray(root); len(rows) > 0 {
		return rows
	}
	if object, ok := root.(map[string]any); ok {
		preferred := []string{"data", "daily", "daily_usage", "daily_counts", "usage", "results", "items"}
		for _, key := range preferred {
			for actual, value := range object {
				if normalizeAnalyticsKey(actual) != normalizeAnalyticsKey(key) {
					continue
				}
				if rows := analyticsRowsFromArray(value); len(rows) > 0 {
					return rows
				}
				if rows := analyticsRows(value); len(rows) > 0 {
					return rows
				}
			}
		}
		if analyticsHasMeasurement(object) {
			return []map[string]any{object}
		}
		for _, value := range object {
			if rows := analyticsRows(value); len(rows) > 0 {
				return rows
			}
		}
	}
	return nil
}

func analyticsRowsFromArray(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok || !analyticsHasMeasurement(row) {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

func analyticsHasMeasurement(row map[string]any) bool {
	return analyticsNodeHasMeasurement(row)
}

func analyticsNodeHasMeasurement(node any) bool {
	switch value := node.(type) {
	case map[string]any:
		for key, raw := range value {
			normalized := normalizeAnalyticsKey(key)
			if _, ok := analyticsCreditKeys[normalized]; ok {
				return true
			}
			if _, ok := analyticsInputKeys[normalized]; ok {
				return true
			}
			if _, ok := analyticsCachedInputKeys[normalized]; ok {
				return true
			}
			if _, ok := analyticsOutputKeys[normalized]; ok {
				return true
			}
			if _, ok := analyticsTotalTokenKeys[normalized]; ok {
				return true
			}
			if analyticsNodeHasMeasurement(raw) {
				return true
			}
		}
	case []any:
		for _, raw := range value {
			if analyticsNodeHasMeasurement(raw) {
				return true
			}
		}
	}
	return false
}

func analyticsNumber(node any, keys map[string]struct{}) (float64, bool) {
	switch value := node.(type) {
	case map[string]any:
		for key, raw := range value {
			if _, ok := keys[normalizeAnalyticsKey(key)]; ok {
				if number, ok := analyticsNumberValue(raw); ok {
					return number, true
				}
			}
		}
		for _, raw := range value {
			if number, ok := analyticsNumber(raw, keys); ok {
				return number, true
			}
		}
	case []any:
		for _, raw := range value {
			if number, ok := analyticsNumber(raw, keys); ok {
				return number, true
			}
		}
	}
	return 0, false
}

func analyticsNumberValue(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	case float64:
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	case int64:
		return float64(number), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	default:
		return 0, false
	}
}

func normalizeAnalyticsKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "", "-", "", " ", "").Replace(value)
	return value
}

func nonNegativeInt64(value float64) int64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value >= float64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1)
	}
	return int64(value)
}

var _ SharedQuotaOfficialAnalyticsSource = (*OpenAIQuotaService)(nil)

// Keep the compile-time contract local to the quota package; the shared pool
// treats this interface as optional so existing test doubles and deployments
// without Analytics continue to use provider-percent fallback.
type SharedQuotaOfficialAnalyticsSource interface {
	QueryAnalytics(ctx context.Context, accountID int64, startDate, endDate time.Time) (*OpenAIAnalyticsUsage, error)
}
