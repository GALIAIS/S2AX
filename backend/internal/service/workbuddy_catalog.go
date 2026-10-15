package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
)

func DefaultWorkBuddyModelIDs(platform string) []string {
	if platform == PlatformWorkBuddyAI {
		return []string{"default-model", "fast-model", "balanced-model", "primary-model", "deep-model", "gpt-5.5", "gpt-5.4", "gpt-5.3-codex", "gemini-3.1-pro", "gemini-3.5-flash", "glm-5.3", "glm-5.2", "hy3", "kimi-k3", "kimi-k2.6", "minimax-m3"}
	}
	return []string{"auto", "hy4-preview-f", "hy3", "hy3-x", "deepseek-v4.1-flash", "glm-5.3", "glm-5.3-flash", "glm-5.2", "glm-5.1", "glm-5v-turbo", "minimax-m3", "kimi-k3-1", "kimi-k2.7", "kimi-k2.6", "deepseek-v4-pro"}
}

func (s *WorkBuddyService) Models(ctx context.Context, account *Account) (*UpstreamModelCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	a, err := s.FreshAccount(ctx, account)
	if err != nil {
		return nil, err
	}
	proxy, err := s.proxyURL(ctx, a.ProxyID)
	if err != nil {
		return nil, err
	}
	data, err := s.call(ctx, a.Platform, proxy, http.MethodGet, "/v3/config", a.Credentials, http.Header{"User-Agent": {"CLI/2.0.0 WorkBuddy/" + workBuddyVersion}}, nil)
	if err != nil {
		return nil, err
	}
	details := make(map[string]map[string]any)
	models, _ := data["models"].([]any)
	for _, raw := range models {
		if m, ok := raw.(map[string]any); ok {
			details[workBuddyString(m["id"])] = m
		}
	}
	out := &UpstreamModelCatalog{Models: []string{}, Metadata: make(map[string]UpstreamModelMetadata)}
	agents, _ := data["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if workBuddyString(agent["name"]) != "cli" {
			continue
		}
		ids, _ := agent["models"].([]any)
		for _, rawID := range ids {
			id := workBuddyString(rawID)
			if id == "" || slices.Contains(out.Models, id) {
				continue
			}
			out.Models = append(out.Models, id)
			d := details[id]
			m := UpstreamModelMetadata{ID: id, DisplayName: workBuddyString(d["name"]), InputModalities: []string{"text"}}
			if m.DisplayName == "" {
				m.DisplayName = id
			}
			input, _ := workBuddyNumber(d["maxInputTokens"])
			output, _ := workBuddyNumber(d["maxOutputTokens"])
			m.ContextWindow = int64(input)
			m.MaxContextWindow = int64(input)
			m.MaxOutputTokens = int64(output)
			if d["supportsImages"] == true && !(a.Platform == PlatformWorkBuddyAI && strings.HasPrefix(strings.ToLower(id), "glm-")) {
				m.InputModalities = append(m.InputModalities, "image")
			}
			reasoning, _ := d["reasoning"].(map[string]any)
			efforts, _ := reasoning["supportedEfforts"].([]any)
			for _, rawEffort := range efforts {
				effort := workBuddyString(rawEffort)
				if effort == "" || (effort == "none" && (d["onlyReasoning"] == true || reasoning["canDisableThinking"] == false)) {
					continue
				}
				if !slices.Contains(m.SupportedReasoningLevels, effort) {
					m.SupportedReasoningLevels = append(m.SupportedReasoningLevels, effort)
				}
			}
			if len(m.SupportedReasoningLevels) > 0 && d["onlyReasoning"] != true && reasoning["canDisableThinking"] != false && !slices.Contains(m.SupportedReasoningLevels, "none") {
				m.SupportedReasoningLevels = append([]string{"none"}, m.SupportedReasoningLevels...)
			}
			hasReasoning := len(m.SupportedReasoningLevels) > 0
			m.Reasoning = &hasReasoning
			m.CreditRate, m.CreditRateBeforeDiscount = workBuddyModelRate(d, data["modelPromotions"], time.Now())
			out.Metadata[id] = m
		}
	}
	if len(out.Models) == 0 {
		return nil, errors.New("WorkBuddy config contains no CLI models for this account")
	}
	if a.ID > 0 && s.accounts != nil {
		snapshot := UpstreamModelMetadataSnapshot{Source: "workbuddy/v3/config", SyncedAt: time.Now().UTC().Format(time.RFC3339), Models: out.Metadata}
		if err := s.accounts.UpdateExtra(ctx, a.ID, map[string]any{UpstreamModelMetadataExtraKey: snapshot}); err != nil {
			return nil, errors.New("failed to save WorkBuddy model catalog")
		}
		account.SetUpstreamModelMetadataSnapshot(snapshot)
	}
	return out, nil
}

// 积分倍率仅供展示，不作为美元定价；按小时促销保留基础倍率，避免缓存跨时段误报免费。
func workBuddyModelRate(model map[string]any, rawPromotions any, now time.Time) (*float64, *float64) {
	parse := func(v any) *float64 {
		text := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(workBuddyString(v)), "x"), "x")
		if text == "" {
			return nil
		}
		n, err := workBuddyNumber(text)
		if err != nil {
			return nil
		}
		return &n
	}
	base := parse(model["credits"])
	rate := base
	promotions, _ := rawPromotions.([]any)
	for _, raw := range promotions {
		p, _ := raw.(map[string]any)
		if p["enabled"] == false || p["kind"] != "discount" {
			continue
		}
		ids, _ := p["modelIds"].([]any)
		if !slices.Contains(ids, model["id"]) {
			continue
		}
		schedule, _ := p["schedule"].(map[string]any)
		if from, err := time.Parse(time.RFC3339, workBuddyString(schedule["validFrom"])); err == nil && now.Before(from) {
			continue
		}
		if until, err := time.Parse(time.RFC3339, workBuddyString(schedule["validUntil"])); err == nil && !now.Before(until) {
			continue
		}
		if daily, _ := schedule["daily"].([]any); len(daily) > 0 {
			if base != nil && *base > 0 {
				return base, nil
			}
			return nil, nil
		}
		discount, _ := p["discount"].(map[string]any)
		r := parse(discount["discountedCredits"])
		if r == nil && base != nil {
			if factor := parse(discount["factor"]); factor != nil {
				v := *factor * *base
				r = &v
			}
		}
		if r != nil && (rate == nil || *r < *rate) {
			rate = r
		}
	}
	if base != nil && rate != nil && *rate < *base {
		return rate, base
	}
	return rate, nil
}

func workBuddyModelsBody(catalog *UpstreamModelCatalog) ([]byte, error) {
	data := make([]map[string]any, 0, len(catalog.Models))
	for _, id := range catalog.Models {
		m := catalog.Metadata[id]
		data = append(data, map[string]any{"id": id, "object": "model", "display_name": m.DisplayName, "owned_by": "workbuddy", "context_window": m.ContextWindow, "max_output_tokens": m.MaxOutputTokens, "supported_reasoning_levels": m.SupportedReasoningLevels, "input_modalities": m.InputModalities})
	}
	return json.Marshal(map[string]any{"object": "list", "data": data})
}

type WorkBuddyUsage struct {
	Unit            string     `json:"unit"`
	Plan            string     `json:"plan"`
	Total           float64    `json:"total"`
	Used            float64    `json:"used"`
	Remaining       float64    `json:"remaining"`
	ResetsAt        *time.Time `json:"resets_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	ExpiringCredits float64    `json:"expiring_credits,omitempty"`
	ExpiryWarning   string     `json:"expiry_warning,omitempty"`
}

func (s *WorkBuddyService) Usage(ctx context.Context, account *Account) (*WorkBuddyUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	a, err := s.FreshAccount(ctx, account)
	if err != nil {
		return nil, err
	}
	proxy, err := s.proxyURL(ctx, a.ProxyID)
	if err != nil {
		return nil, err
	}
	data, err := s.call(ctx, a.Platform, proxy, http.MethodPost, "/billing/meter/get-user-resource-summary", a.Credentials, nil, map[string]any{})
	if err != nil {
		return nil, err
	}
	out := &WorkBuddyUsage{Unit: "credits", Plan: "Free"}
	if data["IsPaidUser"] == true {
		out.Plan = "Pro"
	}
	packages, ok := data["Packages"].([]any)
	if !ok && !(a.GetCredential("enterprise_id") != "" && data["Packages"] == nil) {
		return nil, errors.New("WorkBuddy usage response has no package list")
	}
	for _, raw := range packages {
		p, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("invalid WorkBuddy usage package")
		}
		total, err := workBuddyNumber(p["CycleTotalCapacity"])
		if err != nil {
			return nil, err
		}
		used, err := workBuddyNumber(p["CycleUsedCapacity"])
		if err != nil {
			return nil, err
		}
		out.Total += total
		out.Used += used
	}
	if len(packages) == 0 && a.GetCredential("enterprise_id") != "" {
		usage, err := s.call(ctx, a.Platform, proxy, http.MethodPost, "/billing/meter/get-enterprise-user-usage", a.Credentials, nil, map[string]any{})
		if err != nil {
			return nil, err
		}
		out.Plan = "Enterprise"
		if usage["limitNum"] == nil || usage["credit"] == nil {
			return nil, errors.New("incomplete WorkBuddy enterprise usage")
		}
		out.Total, err = workBuddyNumber(usage["limitNum"])
		if err != nil {
			return nil, err
		}
		out.Used, err = workBuddyNumber(usage["credit"])
		if err != nil {
			return nil, err
		}
		out.ResetsAt = workBuddyTime(usage["cycleResetTime"])
	} else if len(packages) > 0 {
		if err := s.packageExpiry(ctx, a, proxy, packages, out); err != nil {
			out.ExpiresAt = nil
			out.ExpiringCredits = 0
			out.ExpiryWarning = "WorkBuddy package expiry is unavailable"
		}
	}
	out.Remaining = math.Max(0, out.Total-out.Used)
	return out, nil
}

func workBuddyTime(v any) *time.Time {
	text := workBuddyString(v)
	if text == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return &t
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", text, time.FixedZone("UTC+8", 8*3600)); err == nil {
		return &t
	}
	if ms, err := workBuddyNumber(v); err == nil && ms > 0 {
		t := time.UnixMilli(int64(ms))
		return &t
	}
	return nil
}

var workBuddyPackageCodes = [][]string{
	{"TCACA_code_002_AkiJS3ZHF5", "TCACA_code_005_maRGyrHhw1", "TCACA_code_003_FAnt7lcmRT", "TCACA_code_023_4xbGhMrE6q", "TCACA_code_026_BaESVICNoi", "TCACA_code_027_0FCGVA6vSa", "TCACA_code_009_0XmEQc2xOf", "TCACA_code_038_OhvqZtiPKr", "TCACA_code_036_lupO5WgNdG"},
	{"TCACA_code_001_PqouKr6QWV", "TCACA_code_008_cfWoLwvjU4", "TCACA_code_035_ArVxJcGDsm", "TCACA_code_006_DbXS0lrypC", "TCACA_code_039_KRcQj7wUat", "TCACA_code_040_mi9rCYg46x", "TCACA_code_007_nzdH5h4Nl0", "TCACA_code_028_NtpWi0jzXs", "TCACA_code_037_WxOD3MpI2o", "TCACA_code_029_6wCGEWquYy", "TCACA_code_030_BjSt89qTvr"},
}

func (s *WorkBuddyService) packageExpiry(ctx context.Context, a *Account, proxy string, packages []any, out *WorkBuddyUsage) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	counted := make(map[string]bool)
	for _, raw := range packages {
		if p, ok := raw.(map[string]any); ok {
			counted[workBuddyString(p["PackageCode"])] = true
		}
	}
	day := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02")
	for index, suffix := range []string{"paid", "free"} {
		for page, seen := 1, 0; page <= 10; page++ {
			body := map[string]any{"PageNumber": page, "PageSize": 200, "Status": []int{0, 3}, "PackageCodes": workBuddyPackageCodes[index]}
			if index == 0 {
				body["NeedRenewInfo"] = true
			} else {
				body["SlicePeriodStartTime"] = day + " 00:00:00"
				body["SlicePeriodEndTime"] = day + " 23:59:59"
			}
			data, err := s.call(ctx, a.Platform, proxy, http.MethodPost, "/billing/meter/get-user-resource-"+suffix+"-packages", a.Credentials, nil, body)
			if err != nil {
				return err
			}
			items, ok := data["Accounts"].([]any)
			if !ok {
				return errors.New("invalid WorkBuddy package detail list")
			}
			for _, raw := range items {
				p, _ := raw.(map[string]any)
				code := workBuddyString(p["PackageCode"])
				if !counted[code] || code == workBuddyPackageCodes[1][0] || workBuddyString(p["CapacityType"]) == "4" {
					continue
				}
				leftRaw, exists := p["CycleCapacityRemainPrecise"]
				if !exists {
					leftRaw = p["CycleCapacityRemain"]
				}
				left, err := workBuddyNumber(leftRaw)
				if err != nil {
					return err
				}
				end := workBuddyTime(p["DeductionEndTime"])
				if end == nil {
					end = workBuddyTime(p["ExpiredTime"])
				}
				if end == nil {
					end = workBuddyTime(p["CycleEndTime"])
				}
				if left <= 0 || end == nil || !end.After(time.Now()) {
					continue
				}
				if out.ExpiresAt == nil || end.Before(*out.ExpiresAt) {
					out.ExpiresAt = end
					out.ExpiringCredits = left
				} else if end.Equal(*out.ExpiresAt) {
					out.ExpiringCredits += left
				}
			}
			seen += len(items)
			total, _ := workBuddyNumber(data["TotalCount"])
			if len(items) < 200 || (total > 0 && seen >= int(total)) {
				break
			}
		}
	}
	return nil
}
