package service

import (
	"context"
	"strings"
)

// resolveAccountStatsCost 计算账号统计定价费用。
// 返回 nil 表示不覆盖，使用默认公式（total_cost × account_rate_multiplier）。
//
// 优先级（先命中为准）：
//  1. 自定义规则（始终尝试，不依赖 ApplyPricingToAccountStats 开关）
//  2. ApplyPricingToAccountStats 启用时，直接使用本次请求的客户计费（倍率前的 totalCost）
//  3. 模型定价文件（LiteLLM）中上游模型的默认价格
//  4. nil → 走默认公式（total_cost × account_rate_multiplier）
//
// upstreamModel 是最终发往上游的模型 ID。
// totalCost 是本次请求的客户计费（倍率前），用于优先级 2。
// serviceTier 是最终参与用户计费的 OpenAI 服务层级，用于优先级 3。
func resolveAccountStatsCost(
	ctx context.Context,
	channelService *ChannelService,
	billingService *BillingService,
	accountID int64,
	groupID int64,
	upstreamModel string,
	tokens UsageTokens,
	requestCount int,
	totalCost float64,
	serviceTier ...string,
) *float64 {
	if channelService == nil || upstreamModel == "" {
		return nil
	}
	channel, err := channelService.GetChannelForGroup(ctx, groupID)
	if err != nil || channel == nil {
		return nil
	}

	platform := channelService.GetGroupPlatform(ctx, groupID)

	// 优先级 1：自定义规则（始终尝试）。保留服务档位与按次/图片层级。
	tier := optionalServiceTier(serviceTier)
	if cost := tryCustomRules(channel, accountID, groupID, platform, upstreamModel, tokens, requestCount, serviceTier...); cost != nil {
		return cost
	}

	// 优先级 2：渠道开启"应用模型定价到账号统计"时，直接使用客户计费（倍率前）
	if channel.ApplyPricingToAccountStats {
		cost := totalCost
		if cost <= 0 {
			return nil
		}
		return &cost
	}

	// 优先级 3：模型定价文件（LiteLLM）默认价格
	if billingService != nil {
		return tryModelFilePricing(billingService, upstreamModel, tokens, tier)
	}

	return nil
}

// tryModelFilePricing 使用模型定价文件（LiteLLM/fallback）中的标准价格计算费用。
func tryModelFilePricing(billingService *BillingService, model string, tokens UsageTokens, serviceTier ...string) *float64 {
	pricing, err := billingService.GetModelPricing(model)
	if err != nil || pricing == nil {
		return nil
	}
	tier := optionalServiceTier(serviceTier)
	breakdown, err := billingService.CalculateCostWithServiceTier(model, tokens, 1, tier)
	if err != nil || breakdown == nil || breakdown.TotalCost <= 0 {
		return nil
	}
	return &breakdown.TotalCost
}

// tryCustomRules 遍历自定义规则，按数组顺序先命中为准。
func tryCustomRules(
	channel *Channel, accountID, groupID int64,
	platform, model string, tokens UsageTokens, requestCount int, serviceTier ...string,
) *float64 {
	modelLower := strings.ToLower(model)
	for _, rule := range channel.AccountStatsPricingRules {
		if !matchAccountStatsRule(&rule, accountID, groupID) {
			continue
		}
		pricing := findPricingForModel(rule.Pricing, platform, modelLower)
		if pricing == nil {
			continue // 规则匹配但模型不在规则定价中，继续下一条
		}
		return calculateStatsCost(pricing, tokens, requestCount, serviceTier...)
	}
	return nil
}

// matchAccountStatsRule 检查规则是否匹配指定的 accountID 和 groupID。
// 匹配条件：accountID ∈ rule.AccountIDs 或 groupID ∈ rule.GroupIDs。
// 如果规则的 AccountIDs 和 GroupIDs 都为空，视为不匹配。
func matchAccountStatsRule(rule *AccountStatsPricingRule, accountID, groupID int64) bool {
	if len(rule.AccountIDs) == 0 && len(rule.GroupIDs) == 0 {
		return false
	}
	for _, id := range rule.AccountIDs {
		if id == accountID {
			return true
		}
	}
	for _, id := range rule.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

// findPricingForModel 在定价列表中查找匹配的模型定价。
// 先精确匹配，再通配符匹配（按配置顺序，先匹配先使用）。
func findPricingForModel(pricingList []ChannelModelPricing, platform, modelLower string) *ChannelModelPricing {
	// 精确匹配优先
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			if strings.ToLower(m) == modelLower {
				return p
			}
		}
	}
	// 通配符匹配：按配置顺序，先匹配先使用
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			ml := strings.ToLower(m)
			if !strings.HasSuffix(ml, "*") {
				continue
			}
			prefix := strings.TrimSuffix(ml, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return p
			}
		}
	}
	return nil
}

// isPlatformMatch 判断平台是否匹配（空平台视为不限平台）。
func isPlatformMatch(queryPlatform, pricingPlatform string) bool {
	if queryPlatform == "" || pricingPlatform == "" {
		return true
	}
	return queryPlatform == pricingPlatform
}

// calculateStatsCost 使用给定的定价计算费用（不含任何倍率，原始费用）。
func calculateStatsCost(pricing *ChannelModelPricing, tokens UsageTokens, requestCount int, serviceTier ...string) *float64 {
	if pricing == nil {
		return nil
	}
	tokens = normalizeUsageTokens(tokens)
	sizeTier := optionalAccountStatsSizeTier(serviceTier)
	switch pricing.BillingMode {
	case BillingModePerRequest, BillingModeImage:
		return calculatePerRequestStatsCost(pricing, tokens, requestCount, sizeTier)
	default:
		return calculateTokenStatsCost(pricing, tokens, optionalServiceTier(serviceTier))
	}
}

// calculatePerRequestStatsCost 按次/图片计费。
func calculatePerRequestStatsCost(pricing *ChannelModelPricing, tokens UsageTokens, requestCount int, sizeTier string) *float64 {
	var unitPrice *float64
	if strings.TrimSpace(sizeTier) != "" {
		if interval := pricing.GetTierByLabel(sizeTier); interval != nil {
			unitPrice = interval.PerRequestPrice
		}
	}
	if unitPrice == nil {
		totalTokens := tokens.InputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens
		if interval := FindMatchingInterval(pricing.Intervals, totalTokens); interval != nil {
			unitPrice = interval.PerRequestPrice
		}
	}
	if unitPrice == nil {
		unitPrice = pricing.PerRequestPrice
	}
	if unitPrice == nil {
		return nil
	}
	cost := *unitPrice * float64(requestCount)
	return &cost
}

// calculateTokenStatsCost Token 计费。
// Interval selection uses the same context-token definition as the actual
// customer billing path (input + cache tokens), so account statistics do not
// select a different price tier merely because output is large.
func calculateTokenStatsCost(pricing *ChannelModelPricing, tokens UsageTokens, serviceTier ...string) *float64 {
	p := pricing
	if len(pricing.Intervals) > 0 {
		totalTokens := tokens.InputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens
		if iv := FindMatchingInterval(pricing.Intervals, totalTokens); iv != nil {
			merged := *pricing
			merged.Intervals = nil
			if iv.InputPrice != nil {
				merged.InputPrice = iv.InputPrice
			}
			if iv.InputPricePriority != nil {
				merged.InputPricePriority = iv.InputPricePriority
			}
			if iv.OutputPrice != nil {
				merged.OutputPrice = iv.OutputPrice
			}
			if iv.OutputPricePriority != nil {
				merged.OutputPricePriority = iv.OutputPricePriority
			}
			if iv.CacheWritePrice != nil {
				merged.CacheWritePrice = iv.CacheWritePrice
			}
			if iv.CacheWritePricePriority != nil {
				merged.CacheWritePricePriority = iv.CacheWritePricePriority
			}
			if iv.CacheReadPrice != nil {
				merged.CacheReadPrice = iv.CacheReadPrice
			}
			if iv.CacheReadPricePriority != nil {
				merged.CacheReadPricePriority = iv.CacheReadPricePriority
			}
			if iv.PerRequestPrice != nil {
				merged.PerRequestPrice = iv.PerRequestPrice
			}
			if iv.CacheWrite1hPrice != nil {
				merged.CacheWrite1hPrice = iv.CacheWrite1hPrice
			}
			if iv.InputMultiplier != nil {
				merged.InputPrice = multiplyStatsPrice(merged.InputPrice, iv.InputMultiplier)
			}
			if iv.OutputMultiplier != nil {
				merged.OutputPrice = multiplyStatsPrice(merged.OutputPrice, iv.OutputMultiplier)
			}
			if iv.CacheWriteMultiplier != nil {
				merged.CacheWritePrice = multiplyStatsPrice(merged.CacheWritePrice, iv.CacheWriteMultiplier)
			}
			if iv.CacheReadMultiplier != nil {
				merged.CacheReadPrice = multiplyStatsPrice(merged.CacheReadPrice, iv.CacheReadMultiplier)
			}
			p = &merged
		}
	}
	deref := func(ptr *float64) float64 {
		if ptr == nil {
			return 0
		}
		return *ptr
	}
	tier := optionalServiceTier(serviceTier)
	inputPrice := selectServiceTierPrice(p.InputPrice, p.InputPricePriority, tier)
	outputPrice := selectServiceTierPrice(p.OutputPrice, p.OutputPricePriority, tier)
	cacheWritePrice := selectServiceTierPrice(p.CacheWritePrice, p.CacheWritePricePriority, tier)
	cacheReadPrice := selectServiceTierPrice(p.CacheReadPrice, p.CacheReadPricePriority, tier)
	imageInputPrice := deref(p.ImageInputPrice)
	if p.ImageInputPrice == nil {
		imageInputPrice = inputPrice
	}
	imageInputTokens := tokens.ImageInputTokens
	textInputTokens := tokens.InputTokens - imageInputTokens
	if textInputTokens < 0 {
		textInputTokens = 0
		imageInputTokens = tokens.InputTokens
	}
	textOutputTokens := tokens.OutputTokens - tokens.ImageOutputTokens
	if textOutputTokens < 0 {
		textOutputTokens = 0
	}
	cacheCreationCost := float64(tokens.CacheCreationTokens) * cacheWritePrice
	if p.CacheWrite1hPrice != nil {
		cache5m, cache1h := normalizeCacheCreationBreakdown(tokens)
		if cache5m > 0 || cache1h > 0 {
			cacheCreationCost = float64(cache5m)*cacheWritePrice +
				float64(cache1h)*deref(p.CacheWrite1hPrice)
		}
	}
	cost := float64(textInputTokens)*inputPrice +
		float64(imageInputTokens)*imageInputPrice +
		float64(textOutputTokens)*outputPrice +
		cacheCreationCost +
		float64(tokens.CacheReadTokens)*cacheReadPrice +
		float64(tokens.ImageOutputTokens)*deref(p.ImageOutputPrice)
	hasExplicitPrice := p.InputPrice != nil || p.InputPricePriority != nil ||
		p.OutputPrice != nil || p.OutputPricePriority != nil ||
		p.CacheWritePrice != nil || p.CacheWritePricePriority != nil || p.CacheWrite1hPrice != nil ||
		p.CacheReadPrice != nil || p.CacheReadPricePriority != nil ||
		p.ImageInputPrice != nil || p.ImageOutputPrice != nil
	if cost <= 0 && !hasExplicitPrice {
		return nil
	}
	return &cost
}

func selectServiceTierPrice(standard, priority *float64, serviceTier string) float64 {
	if normalizeBillingServiceTier(serviceTier) == "priority" && priority != nil {
		return *priority
	}
	if standard == nil {
		return 0
	}
	return *standard
}

// multiplyStatsPrice 将区间倍率应用于基础价格，缺失基础价格时保持未配置状态。
func multiplyStatsPrice(price, multiplier *float64) *float64 {
	if price == nil || multiplier == nil {
		return price
	}
	value := *price * *multiplier
	return &value
}

func optionalServiceTier(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func optionalAccountStatsSizeTier(values []string) string {
	if len(values) < 2 {
		return ""
	}
	return values[1]
}

// applyAccountStatsCost resolves the account stats cost for a usage log entry.
// It resolves the upstream model (falling back to the requested model) and calls
// the 4-level priority chain via resolveAccountStatsCost.
func applyAccountStatsCost(
	ctx context.Context,
	usageLog *UsageLog,
	cs *ChannelService, bs *BillingService,
	accountID int64, groupID int64,
	upstreamModel, requestedModel string,
	tokens UsageTokens,
	totalCost float64,
	serviceTier string,
) {
	model := upstreamModel
	if model == "" {
		model = requestedModel
	}
	requestCount := 1
	if usageLog != nil {
		if usageLog.ImageCount > 0 {
			requestCount = usageLog.ImageCount
		} else if usageLog.VideoCount > 0 {
			requestCount = usageLog.VideoCount
		}
	}
	sizeTier := ""
	if usageLog != nil {
		if usageLog.BillingTier != nil {
			sizeTier = strings.TrimSpace(*usageLog.BillingTier)
		}
		if sizeTier == "" && usageLog.ImageSize != nil {
			sizeTier = strings.TrimSpace(*usageLog.ImageSize)
		}
		if sizeTier == "" && usageLog.VideoResolution != nil {
			sizeTier = strings.TrimSpace(*usageLog.VideoResolution)
		}
	}
	effectiveServiceTier := serviceTier
	if usageLog != nil && usageLog.ServiceTier != nil {
		effectiveServiceTier = *usageLog.ServiceTier
	}
	usageLog.AccountStatsCost = resolveAccountStatsCost(
		ctx, cs, bs, accountID, groupID, model, tokens, requestCount, totalCost, effectiveServiceTier,
		sizeTier,
	)
}
