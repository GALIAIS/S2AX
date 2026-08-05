package service

import (
	"context"
	"log/slog"
)

// PricingSource 定价来源标识
const (
	PricingSourceChannel  = "channel"
	PricingSourceLiteLLM  = "litellm"
	PricingSourceFallback = "fallback"
)

// ResolvedPricing 统一定价解析结果
type ResolvedPricing struct {
	// Mode 计费模式
	Mode BillingMode

	// Token 模式：基础定价（来自 LiteLLM 或 fallback）
	BasePricing *ModelPricing

	// Token 模式：区间定价列表（如有，覆盖 BasePricing 中的对应字段）
	Intervals []PricingInterval

	// 按次/图片模式：分层定价
	RequestTiers []PricingInterval

	// 按次/图片模式：默认价格（未命中层级时使用）
	DefaultPerRequestPrice    float64
	DefaultPerRequestPriceSet bool

	// 来源标识
	Source string // "channel", "litellm", "fallback"

	// 是否支持缓存细分
	SupportsCacheBreakdown bool

	// 渠道定价原始配置（用于区间模式下获取 ImageOutputPrice）
	channelPricing *ChannelModelPricing
}

// ModelPricingResolver 统一模型定价解析器。
// 解析链：Channel → LiteLLM → Fallback。
type ModelPricingResolver struct {
	channelService *ChannelService
	billingService *BillingService
}

// NewModelPricingResolver 创建定价解析器实例
func NewModelPricingResolver(channelService *ChannelService, billingService *BillingService) *ModelPricingResolver {
	return &ModelPricingResolver{
		channelService: channelService,
		billingService: billingService,
	}
}

// PricingInput 定价解析输入
type PricingInput struct {
	Model   string
	GroupID *int64 // nil 表示不检查渠道
}

// Resolve 解析模型定价。
// 1. 获取基础定价（LiteLLM → Fallback）
// 2. 如果指定了 GroupID，查找渠道定价并覆盖
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
	var chPricing *ChannelModelPricing
	if input.GroupID != nil && r.channelService != nil {
		chPricing = r.channelService.GetChannelModelPricing(ctx, *input.GroupID, input.Model)
		if chPricing != nil {
			mode := chPricing.BillingMode
			if mode == "" {
				mode = BillingModeToken
			}
			if mode == BillingModePerRequest || mode == BillingModeImage {
				resolved := &ResolvedPricing{
					Mode:           mode,
					Source:         PricingSourceChannel,
					channelPricing: chPricing,
				}
				r.applyRequestTierOverrides(chPricing, resolved)
				return resolved
			}
		}
	}

	// 1. 获取基础定价
	basePricing, source := r.resolveBasePricing(input.Model)

	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		BasePricing:            basePricing,
		Source:                 source,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
	}

	// 2. 如果有 GroupID，尝试渠道覆盖
	if chPricing != nil {
		resolved.Source = PricingSourceChannel
		resolved.channelPricing = chPricing
		r.applyTokenOverrides(chPricing, resolved)
	} else if input.GroupID != nil {
		r.applyChannelOverrides(ctx, *input.GroupID, input.Model, resolved)
	}

	return resolved
}

// resolveBasePricing 从 LiteLLM 或 Fallback 获取基础定价
func (r *ModelPricingResolver) resolveBasePricing(model string) (*ModelPricing, string) {
	pricing, err := r.billingService.GetModelPricing(model)
	if err != nil {
		slog.Debug("failed to get model pricing from LiteLLM, using fallback",
			"model", model, "error", err)
		return nil, PricingSourceFallback
	}
	return pricing, PricingSourceLiteLLM
}

// applyChannelOverrides 应用渠道定价覆盖
func (r *ModelPricingResolver) applyChannelOverrides(ctx context.Context, groupID int64, model string, resolved *ResolvedPricing) {
	chPricing := r.channelService.GetChannelModelPricing(ctx, groupID, model)
	if chPricing == nil {
		return
	}

	resolved.Source = PricingSourceChannel
	resolved.channelPricing = chPricing
	resolved.Mode = chPricing.BillingMode
	if resolved.Mode == "" {
		resolved.Mode = BillingModeToken
	}

	switch resolved.Mode {
	case BillingModeToken:
		r.applyTokenOverrides(chPricing, resolved)
	case BillingModePerRequest, BillingModeImage:
		r.applyRequestTierOverrides(chPricing, resolved)
	}
}

// applyTokenOverrides 应用 token 模式的渠道覆盖
func (r *ModelPricingResolver) applyTokenOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	// 过滤掉所有价格字段都为空的无效 interval
	validIntervals := filterValidIntervals(chPricing.Intervals)

	// 如果有有效的区间定价，使用区间
	if len(validIntervals) > 0 {
		resolved.Intervals = validIntervals
		// 区间不匹配时回退到 BasePricing；flat 价格也必须覆盖，避免
		// 配置了默认回退价却只在命中区间时生效。
		if resolved.BasePricing == nil {
			resolved.BasePricing = &ModelPricing{}
		} else {
			// 防止修改 fallbackPrices 中的共享指针
			cloned := *resolved.BasePricing
			resolved.BasePricing = &cloned
		}
		applyTokenPriceOverrides(chPricing, resolved.BasePricing)
		return
	}

	// 否则用 flat 字段覆盖 BasePricing
	if resolved.BasePricing == nil {
		resolved.BasePricing = &ModelPricing{}
	} else {
		// 防止修改 fallbackPrices 中的共享指针
		cloned := *resolved.BasePricing
		resolved.BasePricing = &cloned
	}

	applyTokenPriceOverrides(chPricing, resolved.BasePricing)
}

func applyTokenPriceOverrides(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing.InputPrice != nil {
		pricing.InputPricePerToken = *chPricing.InputPrice
		pricing.InputPricePerTokenPriority = *chPricing.InputPrice
		pricing.InputPricePerTokenPrioritySet = true
	}
	if chPricing.InputPricePriority != nil {
		pricing.InputPricePerTokenPriority = *chPricing.InputPricePriority
		pricing.InputPricePerTokenPrioritySet = true
	}
	if chPricing.OutputPrice != nil {
		pricing.OutputPricePerToken = *chPricing.OutputPrice
		pricing.OutputPricePerTokenPriority = *chPricing.OutputPrice
		pricing.OutputPricePerTokenPrioritySet = true
	}
	if chPricing.OutputPricePriority != nil {
		pricing.OutputPricePerTokenPriority = *chPricing.OutputPricePriority
		pricing.OutputPricePerTokenPrioritySet = true
	}
	if chPricing.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *chPricing.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *chPricing.CacheWritePrice
		pricing.CacheCreationPricePerTokenPrioritySet = true
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *chPricing.CacheWritePrice
		pricing.CacheCreation1hPrice = *chPricing.CacheWritePrice
	}
	if chPricing.CacheWritePricePriority != nil {
		pricing.CacheCreationPricePerTokenPriority = *chPricing.CacheWritePricePriority
		pricing.CacheCreationPricePerTokenPrioritySet = true
	}
	if chPricing.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *chPricing.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = *chPricing.CacheReadPrice
		pricing.CacheReadPricePerTokenPrioritySet = true
	}
	if chPricing.CacheReadPricePriority != nil {
		pricing.CacheReadPricePerTokenPriority = *chPricing.CacheReadPricePriority
		pricing.CacheReadPricePerTokenPrioritySet = true
	}
	// 渠道定价覆盖一切：显式配置则用配置值，未配置则归零（不回退到 LiteLLM）
	if chPricing.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *chPricing.ImageOutputPrice
	} else {
		pricing.ImageOutputPricePerToken = 0
	}
	pricing.ImageOutputPriceExplicit = true
	applyChannelImageInputPrice(chPricing, pricing)
}

// applyChannelImageInputPrice 应用渠道图片输入价：显式配置则用配置值；
// 未配置时归零并清除显式标志，使 computeTokenBreakdown 回退到文本输入价，
// 避免渠道自定义定价遗漏图片价时意外沿用 LiteLLM 图片价。
func applyChannelImageInputPrice(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing != nil && chPricing.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *chPricing.ImageInputPrice
		pricing.ImageInputPriceExplicit = true
	} else {
		pricing.ImageInputPricePerToken = 0
		pricing.ImageInputPriceExplicit = false
	}
}

// applyRequestTierOverrides 应用按次/图片模式的渠道覆盖
func (r *ModelPricingResolver) applyRequestTierOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.RequestTiers = filterValidIntervals(chPricing.Intervals)
	if chPricing.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *chPricing.PerRequestPrice
		resolved.DefaultPerRequestPriceSet = true
	}
}

// filterValidIntervals 过滤掉所有价格字段都为空的无效 interval。
// 前端可能创建了只有 min/max 但无价格的空 interval。
func filterValidIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheReadPrice != nil ||
			iv.InputPricePriority != nil || iv.OutputPricePriority != nil ||
			iv.CacheWritePricePriority != nil || iv.CacheReadPricePriority != nil ||
			iv.PerRequestPrice != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// GetIntervalPricing 根据 context token 数获取区间定价。
// 如果有区间列表，找到匹配区间并构造 ModelPricing；否则直接返回 BasePricing。
func (r *ModelPricingResolver) GetIntervalPricing(resolved *ResolvedPricing, totalContextTokens int) *ModelPricing {
	if len(resolved.Intervals) == 0 {
		return resolved.BasePricing
	}

	iv := FindMatchingInterval(resolved.Intervals, totalContextTokens)
	if iv == nil {
		return resolved.BasePricing
	}

	base := resolved.BasePricing
	if base != nil {
		cloned := *base
		base = &cloned
	}
	return intervalToModelPricingWithBase(iv, base, resolved.SupportsCacheBreakdown, resolved.channelPricing)
}

// intervalToModelPricing 将区间定价转换为 ModelPricing
func intervalToModelPricing(iv *PricingInterval, supportsCacheBreakdown bool, chPricing *ChannelModelPricing) *ModelPricing {
	return intervalToModelPricingWithBase(iv, nil, supportsCacheBreakdown, chPricing)
}

// intervalToModelPricingWithBase merges an interval over the flat/base prices.
// Missing fields inherit the base instead of silently becoming free.
func intervalToModelPricingWithBase(iv *PricingInterval, base *ModelPricing, supportsCacheBreakdown bool, chPricing *ChannelModelPricing) *ModelPricing {
	var pricing ModelPricing
	if base != nil {
		pricing = *base
	}
	pricing.SupportsCacheBreakdown = supportsCacheBreakdown
	if iv.InputPrice != nil {
		pricing.InputPricePerToken = *iv.InputPrice
		pricing.InputPricePerTokenPriority = *iv.InputPrice
		pricing.InputPricePerTokenPrioritySet = true
	}
	if iv.InputPricePriority != nil {
		pricing.InputPricePerTokenPriority = *iv.InputPricePriority
		pricing.InputPricePerTokenPrioritySet = true
	}
	if iv.OutputPrice != nil {
		pricing.OutputPricePerToken = *iv.OutputPrice
		pricing.OutputPricePerTokenPriority = *iv.OutputPrice
		pricing.OutputPricePerTokenPrioritySet = true
	}
	if iv.OutputPricePriority != nil {
		pricing.OutputPricePerTokenPriority = *iv.OutputPricePriority
		pricing.OutputPricePerTokenPrioritySet = true
	}
	if iv.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *iv.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *iv.CacheWritePrice
		pricing.CacheCreationPricePerTokenPrioritySet = true
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *iv.CacheWritePrice
		pricing.CacheCreation1hPrice = *iv.CacheWritePrice
	}
	if iv.CacheWritePricePriority != nil {
		pricing.CacheCreationPricePerTokenPriority = *iv.CacheWritePricePriority
		pricing.CacheCreationPricePerTokenPrioritySet = true
	}
	if iv.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *iv.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = *iv.CacheReadPrice
		pricing.CacheReadPricePerTokenPrioritySet = true
	}
	if iv.CacheReadPricePriority != nil {
		pricing.CacheReadPricePerTokenPriority = *iv.CacheReadPricePriority
		pricing.CacheReadPricePerTokenPrioritySet = true
	}
	// 渠道定价存在时，ImageOutputPrice 显式覆盖；图片输入价用渠道级配置
	// （区间不携带图片输入价，与 image_output 一致）。
	if chPricing != nil {
		pricing.ImageOutputPriceExplicit = true
		if chPricing.ImageOutputPrice != nil {
			pricing.ImageOutputPricePerToken = *chPricing.ImageOutputPrice
		} else {
			pricing.ImageOutputPricePerToken = 0
		}
		applyChannelImageInputPrice(chPricing, &pricing)
	}
	return &pricing
}

// GetRequestTierPrice 根据层级标签获取按次价格
func (r *ModelPricingResolver) GetRequestTierPrice(resolved *ResolvedPricing, tierLabel string) float64 {
	price, _ := r.getRequestTierPrice(resolved, tierLabel)
	return price
}

func (r *ModelPricingResolver) getRequestTierPrice(resolved *ResolvedPricing, tierLabel string) (float64, bool) {
	if resolved == nil {
		return 0, false
	}
	for _, tier := range resolved.RequestTiers {
		if tier.TierLabel == tierLabel && tier.PerRequestPrice != nil {
			return *tier.PerRequestPrice, true
		}
	}
	return 0, false
}

// GetRequestTierPriceByContext 根据 context token 数获取按次价格
func (r *ModelPricingResolver) GetRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) float64 {
	price, _ := r.getRequestTierPriceByContext(resolved, totalContextTokens)
	return price
}

func (r *ModelPricingResolver) getRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) (float64, bool) {
	if resolved == nil {
		return 0, false
	}
	iv := FindMatchingInterval(resolved.RequestTiers, totalContextTokens)
	if iv != nil && iv.PerRequestPrice != nil {
		return *iv.PerRequestPrice, true
	}
	return 0, false
}
