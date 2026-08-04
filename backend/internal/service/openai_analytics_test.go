package service

import (
	"testing"
	"time"
)

func TestParseOpenAIAnalyticsUsageAggregatesDailyCreditsAndTokens(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	usage, err := parseOpenAIAnalyticsUsage([]byte(`{
		"data": [
			{"date":"2026-08-01","usage":{"credits":"125.5","input_tokens":1000,"cached_input_tokens":200,"output_tokens":300}},
			{"date":"2026-08-02","credit":74.5,"inputTokens":500,"outputTokens":100},
			{"date":"2026-08-03","status":"partial"}
		]
	}`), start, end)
	if err != nil {
		t.Fatal(err)
	}
	if !usage.CreditsAvailable || usage.Credits != 200 {
		t.Fatalf("credits = %v available=%v, want 200/true", usage.Credits, usage.CreditsAvailable)
	}
	if usage.InputTokens != 1500 || usage.CachedInputTokens != 200 || usage.OutputTokens != 400 || usage.TotalTokens != 2100 {
		t.Fatalf("tokens = input:%d cached:%d output:%d total:%d", usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens, usage.TotalTokens)
	}
	if usage.Status != "available" || usage.Confidence != 1 || usage.RecordCount != 2 {
		t.Fatalf("metadata = %#v", usage)
	}
}

func TestParseOpenAIAnalyticsUsageDoesNotInventCapacityForEmptyData(t *testing.T) {
	usage, err := parseOpenAIAnalyticsUsage([]byte(`{"data":[]}`), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if usage.CreditsAvailable || usage.Credits != 0 || usage.Status != "no_data" {
		t.Fatalf("empty analytics = %#v", usage)
	}
}
