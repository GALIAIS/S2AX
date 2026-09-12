package service

import (
	"context"
	"maps"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	// OpenAIImageTextModelExtraKey 保存 OAuth 生图请求使用的 Responses 外层文本模型，
	// 与 image_generation 工具模型刻意分开。
	OpenAIImageTextModelExtraKey = "openai_image_text_model"
	// OpenAIImageModelExtraKey 保存客户端未提供模型时，Hosted Responses 桥接所用的
	// 默认 image_generation 工具模型。
	OpenAIImageModelExtraKey = "openai_image_model"
	// OpenAIImageModelsExtraKey 保存管理端选择器展示的自定义图片工具模型 ID；该列表
	// 属于账号数据，不是编译进二进制的模型白名单。
	OpenAIImageModelsExtraKey = "openai_image_models"
)

// GetOpenAIImageTextModel 返回配置的 Responses 外层文本模型。为空时由调用方从账号
// 的鉴权模型目录中发现可用文本模型。
func (a *Account) GetOpenAIImageTextModel() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString(OpenAIImageTextModelExtraKey))
}

// GetOpenAIImageModel 返回账号默认的 image_generation 工具模型 ID。模型 ID 可以是
// 供应商自定义值，本地不会对其做规范化。
func (a *Account) GetOpenAIImageModel() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString(OpenAIImageModelExtraKey))
}

// GetOpenAIImageModels 返回配置的图片工具模型 ID，并清理 JSONB 中的空值与重复项，
// 同时保持管理员输入顺序。
func (a *Account) GetOpenAIImageModels() []string {
	if a == nil || a.Extra == nil {
		return nil
	}
	models, _ := openAIImageModelList(a.Extra[OpenAIImageModelsExtraKey])
	return models
}

// ResolveOpenAIImageToolModel 返回 Hosted 桥接需要的默认图片工具模型，优先使用单值
// 配置，再回退到自定义列表首项；未配置时返回空串，让调用方明确跳过自动注入。
func (a *Account) ResolveOpenAIImageToolModel() string {
	if model := a.GetOpenAIImageModel(); model != "" {
		return model
	}
	models := a.GetOpenAIImageModels()
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

// IsOpenAIImageToolModel 识别内置图片模型家族和账号显式配置。显式配置使任意供应商
// 模型 ID 无需修改二进制即可作为图片工具使用。
func (a *Account) IsOpenAIImageToolModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	if isOpenAIImageGenerationModel(model) {
		return true
	}
	for _, configured := range a.GetOpenAIImageModels() {
		if strings.EqualFold(configured, model) {
			return true
		}
	}
	if strings.EqualFold(a.GetOpenAIImageModel(), model) {
		return true
	}
	return false
}

// ValidateOpenAIImageGenerationExtra 在 API 边界校验账号级图片桥接配置。图片模型只
// 要是非空供应商 ID 即可；仅阻止把图片专用模型误填为桥接文本模型。
func ValidateOpenAIImageGenerationExtra(platform string, extra map[string]any) error {
	if platform != PlatformOpenAI || extra == nil {
		return nil
	}

	configuredImages := make([]string, 0)
	if raw, exists := extra[OpenAIImageModelsExtraKey]; exists {
		models, ok := openAIImageModelList(raw)
		if !ok {
			return infraerrors.BadRequest(
				"OPENAI_IMAGE_MODELS_INVALID",
				"openai_image_models must be an array of strings",
			)
		}
		configuredImages = append(configuredImages, models...)
	}
	if raw, exists := extra[OpenAIImageModelExtraKey]; exists {
		model, ok := raw.(string)
		if !ok {
			return infraerrors.BadRequest(
				"OPENAI_IMAGE_MODEL_INVALID",
				"openai_image_model must be a string",
			)
		}
		if strings.TrimSpace(model) != "" {
			configuredImages = append(configuredImages, strings.TrimSpace(model))
		}
	}
	if raw, exists := extra[OpenAIImageTextModelExtraKey]; exists {
		textModel, ok := raw.(string)
		if !ok {
			return infraerrors.BadRequest(
				"OPENAI_IMAGE_TEXT_MODEL_INVALID",
				"openai_image_text_model must be a string",
			)
		}
		textModel = strings.TrimSpace(textModel)
		if textModel != "" {
			if isOpenAIImageGenerationModel(textModel) || containsOpenAIImageModel(configuredImages, textModel) {
				return infraerrors.BadRequest(
					"OPENAI_IMAGE_TEXT_MODEL_IS_IMAGE",
					"openai_image_text_model must be a Responses-capable text model",
				)
			}
		}
	}
	return nil
}

// normalizeOpenAIImageGenerationExtra 统一保存图片桥接配置的字符串格式，空的可选
// 值会被删除，避免后续读取时把空值误当成显式模型。
func normalizeOpenAIImageGenerationExtra(platform string, extra map[string]any) (map[string]any, error) {
	if platform != PlatformOpenAI {
		return extra, nil
	}
	if err := ValidateOpenAIImageGenerationExtra(platform, extra); err != nil {
		return nil, err
	}
	normalized := maps.Clone(extra)
	if normalized == nil {
		return nil, nil
	}
	for _, key := range []string{OpenAIImageTextModelExtraKey, OpenAIImageModelExtraKey} {
		if raw, exists := normalized[key]; exists {
			value := strings.TrimSpace(raw.(string))
			if value == "" {
				delete(normalized, key)
			} else {
				normalized[key] = value
			}
		}
	}
	if raw, exists := normalized[OpenAIImageModelsExtraKey]; exists {
		models, _ := openAIImageModelList(raw)
		if len(models) == 0 {
			delete(normalized, OpenAIImageModelsExtraKey)
		} else {
			normalized[OpenAIImageModelsExtraKey] = models
		}
	}
	return normalized, nil
}

// openAIImageModelList 同时接受 JSON 解码后的 []any 和服务代码使用的 []string，混合
// 非字符串值会被拒绝。
func openAIImageModelList(raw any) ([]string, bool) {
	if raw == nil {
		return nil, true
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []string:
		values = make([]any, len(typed))
		for i, value := range typed {
			values[i] = value
		}
	default:
		return nil, false
	}

	models := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		model, ok := value.(string)
		if !ok {
			return nil, false
		}
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		models = append(models, model)
	}
	return models, true
}

func containsOpenAIImageModel(models []string, candidate string) bool {
	for _, model := range models {
		if strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

// hasOpenAIImageGenerationExtra 判断增量更新是否触及图片桥接配置，避免普通 Extra
// 更新为每个账号额外执行一次查询。
func hasOpenAIImageGenerationExtra(extra map[string]any) bool {
	if extra == nil {
		return false
	}
	_, hasText := extra[OpenAIImageTextModelExtraKey]
	_, hasDefault := extra[OpenAIImageModelExtraKey]
	_, hasList := extra[OpenAIImageModelsExtraKey]
	return hasText || hasDefault || hasList
}

// validateOpenAIImageTextModel 确保图片桥接的 Responses 外层模型不是图片工具模型，
// 但不限制供应商自定义的普通文本模型 ID。
func validateOpenAIImageTextModel(account *Account, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return infraerrors.BadRequest(
			"OPENAI_IMAGE_TEXT_MODEL_REQUIRED",
			"a Responses-capable text model is required for OpenAI image generation",
		)
	}
	if account != nil && account.IsOpenAIImageToolModel(model) {
		return infraerrors.BadRequest(
			"OPENAI_IMAGE_TEXT_MODEL_IS_IMAGE",
			"a Responses-capable text model is required for OpenAI image generation",
		)
	}
	return nil
}

// resolveOpenAIImageTextModel 为账号测试解析图片桥接的外层文本模型。正式服务通过
// 共享模型目录动态选择；没有网关依赖的单元测试保留旧常量作为隔离兜底。
func (s *AccountTestService) resolveOpenAIImageTextModel(ctx context.Context, account *Account, override string) (string, error) {
	if candidate := strings.TrimSpace(override); candidate != "" {
		return candidate, validateOpenAIImageTextModel(account, candidate)
	}
	if candidate := account.GetOpenAIImageTextModel(); candidate != "" {
		return candidate, validateOpenAIImageTextModel(account, candidate)
	}
	if s != nil && s.openaiGatewayService != nil {
		return s.openaiGatewayService.resolveOpenAIImagesResponsesTextModel(ctx, account, "")
	}
	return openAIImagesResponsesMainModel, nil
}
