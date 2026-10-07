package service

import (
	"context"
	"strings"
)

const OpenAIDecisionsBillingModel = "gpt-6-luna-decisions"
const openAIDecisionsPricingSuffix = "-decisions"

type openAIDecisionsPricingKey struct{}

func WithOpenAIDecisionsPricing(ctx context.Context) context.Context {
	return context.WithValue(ctx, openAIDecisionsPricingKey{}, true)
}

func openAIDecisionsPricingModel(ctx context.Context, model string) string {
	if enabled, _ := ctx.Value(openAIDecisionsPricingKey{}).(bool); !enabled {
		return model
	}
	model = strings.TrimSpace(model)
	if model == "" || strings.HasSuffix(model, openAIDecisionsPricingSuffix) {
		return model
	}
	// 同名模型的 Decisions 与生成接口使用独立价格键，覆盖准入、利润门和结算。
	return model + openAIDecisionsPricingSuffix
}
