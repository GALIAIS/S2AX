package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

var codexToolCapabilityFields = []string{
	"supports_search_tool", "apply_patch_tool_type", "comp_hash", "tool_mode", "use_responses_lite",
	"multi_agent_reasoning_effort", "multi_agent_version",
}

func applyCodexToolCapabilities(dst, src map[string]json.RawMessage, overwrite bool) bool {
	changed := false
	for _, field := range codexToolCapabilityFields {
		value := bytes.TrimSpace(src[field])
		if len(value) == 0 {
			continue
		}
		// These Codex fields are nullable booleans or strings, never arbitrary objects.
		if !bytes.Equal(value, []byte("null")) {
			if field == "supports_search_tool" || field == "use_responses_lite" {
				if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
					continue
				}
			} else {
				var text string
				if json.Unmarshal(value, &text) != nil {
					continue
				}
			}
		}
		current, exists := dst[field]
		if (exists && !overwrite) || bytes.Equal(current, value) {
			continue
		}
		dst[field] = append(json.RawMessage(nil), value...)
		changed = true
	}
	return changed
}

func accountCodexToolCapabilities(account *Account, modelID string) map[string]json.RawMessage {
	capabilities := make(map[string]json.RawMessage)
	if account == nil {
		return capabilities
	}
	if metadata, ok := account.GetUpstreamModelMetadata(modelID); ok {
		applyCodexToolCapabilities(capabilities, metadata.CodexToolCapabilities, true)
	}
	if account.IsOpenAI() && shouldForwardOpenAIResponsesViaRawChatCompletions(account) {
		// This bridge implements client-side tool discovery, even without a native manifest.
		applyCodexToolCapabilities(capabilities, map[string]json.RawMessage{"supports_search_tool": json.RawMessage("true")}, false)
	}
	// Codex 0.153's bundled Astra catalog verifies these values. API-key routes
	// use standard Responses, not the ChatGPT-only Responses Lite wire.
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = account.GetOpenAIBaseURL()
	}
	parsed, err := url.Parse(baseURL)
	official := err == nil && (strings.EqualFold(parsed.Hostname(), "api.openai.com") ||
		(account.IsOpenAIOAuth() && strings.EqualFold(parsed.Hostname(), "chatgpt.com")))
	if account.IsOpenAI() && isOpenAIGPT6AstraModel(modelID) && official {
		defaults := map[string]json.RawMessage{
			"supports_search_tool":  json.RawMessage("true"),
			"apply_patch_tool_type": json.RawMessage(`"freeform"`),
			"comp_hash":             json.RawMessage(`"3000"`),
			"tool_mode":             json.RawMessage("null"),
			"use_responses_lite":    json.RawMessage("false"),
		}
		if account.IsOpenAIOAuth() {
			defaults["tool_mode"] = json.RawMessage(`"code_mode_only"`)
			defaults["use_responses_lite"] = json.RawMessage("true")
		}
		applyCodexToolCapabilities(capabilities, defaults, false)
	}
	if account.IsOpenAIApiKey() {
		target := modelID
		if isOpenAIGPT6AstraModel(target) {
			target = "gpt-6-astra"
		}
		_, disabled := apiKeyCodexModelsWithoutResponsesLite[target]
		if disabled && bytes.Equal(capabilities["use_responses_lite"], []byte("true")) {
			capabilities["use_responses_lite"] = json.RawMessage("false")
		}
	}
	return capabilities
}

// codexModelRoutingAccountIDs 返回分组显式为该公开别名声明的账号集合。
//
// 返回非空表示运营者已经用 model_routing 指明“这个别名由这些账号服务”，此时别名的
// 归属不再是需要推断的未知量：能力声明只看这些账号，且不再因为它们映射到不同上游而
// 判定为冲突。返回空表示没有相关规则，保持原有的全量推断与失败即关闭行为。
func codexModelRoutingAccountIDs(group *Group, modelID string) []int64 {
	if group == nil {
		return nil
	}
	return group.GetRoutingAccountIDs(strings.TrimSpace(modelID))
}

func groupCodexModelMetadata(
	platform string,
	modelID string,
	accounts []Account,
	group *Group,
	compositeRoutes []CompositeModelRoute,
	compositeRoutesAvailable bool,
) (codexModelMetadataOverride, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return codexModelMetadataOverride{}, false
	}
	// 显式路由规则本身与平台无关，先取出来供后续两处判定共用。
	routedAccountIDs := codexModelRoutingAccountIDs(group, modelID)
	routed := len(routedAccountIDs) > 0
	upstreamModel := modelID
	if platform == PlatformComposite {
		var resolved bool
		platform, upstreamModel, resolved = resolveCodexCompositeModelTarget(
			modelID,
			accounts,
			compositeRoutes,
			compositeRoutesAvailable,
		)
		if !resolved {
			if !routed && codexExplicitModelTargetsConflict(accounts, modelID) {
				return codexModelMetadataOverride{
					reasoningConflict:       true,
					inputModalitiesConflict: true,
				}, true
			}
			return codexModelMetadataOverride{}, false
		}
	}
	if !isConcreteRequestPlatform(platform) {
		return codexModelMetadataOverride{}, false
	}

	explicitClaims := false
	if upstreamModel == modelID {
		for i := range accounts {
			account := &accounts[i]
			if routed && !containsInt64(routedAccountIDs, account.ID) {
				continue
			}
			if account.Platform == platform && codexExplicitModelMappingClaims(*account, modelID) {
				explicitClaims = true
				break
			}
		}
	}
	// 别名已被显式路由声明时，"多个账号映射到不同上游"是故障转移的正常写法，不是歧义，
	// 因此不再失败即关闭；能力回落到与单账号无快照时相同的按名推导。
	explicitTargetsConflict := explicitClaims && !routed &&
		codexExplicitModelTargetsConflictForPlatform(accounts, platform, modelID)
	publicAlias := upstreamModel != modelID
	candidates := make([]UpstreamModelMetadata, 0)
	missingMetadata := false
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != platform {
			continue
		}
		if routed && !containsInt64(routedAccountIDs, account.ID) {
			continue
		}
		var lookupModel string
		if explicitClaims {
			if !codexExplicitModelMappingClaims(*account, modelID) {
				continue
			}
			lookupModel = account.GetMappedModel(modelID)
		} else {
			if !account.IsModelSupported(upstreamModel) {
				continue
			}
			lookupModel = account.GetMappedModel(upstreamModel)
		}
		if strings.TrimSpace(lookupModel) != modelID {
			publicAlias = true
		}
		metadata, ok := account.GetUpstreamModelMetadata(lookupModel)
		if !ok {
			if explicitTargetsConflict {
				return codexModelMetadataOverride{
					reasoningConflict:       true,
					inputModalitiesConflict: true,
				}, true
			}
			missingMetadata = true
		}
		metadata.CodexToolCapabilities = accountCodexToolCapabilities(account, lookupModel)
		candidates = append(candidates, metadata)
	}
	if len(candidates) == 0 {
		return codexModelMetadataOverride{}, false
	}
	metadata := intersectUpstreamModelMetadata(modelID, candidates)
	if missingMetadata {
		metadata = codexModelMetadataOverride{UpstreamModelMetadata: UpstreamModelMetadata{
			CodexToolCapabilities: metadata.CodexToolCapabilities,
		}}
	}
	if publicAlias {
		metadata.DisplayName = modelID
		metadata.Description = configuredCodexCustomDescription
	}
	return metadata, true
}

func codexExplicitModelTargetsConflict(accounts []Account, modelID string) bool {
	targets := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		mappedModel, matched := account.ResolveMappedModel(modelID)
		mappedModel = strings.TrimSpace(mappedModel)
		if !matched || mappedModel == "" {
			continue
		}
		targets[strings.TrimSpace(account.Platform)+"\x00"+mappedModel] = struct{}{}
	}
	return len(targets) > 1
}

func codexExplicitModelTargetsConflictForPlatform(accounts []Account, platform, modelID string) bool {
	targets := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != platform {
			continue
		}
		mappedModel, matched := account.ResolveMappedModel(modelID)
		mappedModel = strings.TrimSpace(mappedModel)
		if !matched || mappedModel == "" {
			continue
		}
		targets[mappedModel] = struct{}{}
	}
	return len(targets) > 1
}

func intersectUpstreamModelMetadata(modelID string, candidates []UpstreamModelMetadata) codexModelMetadataOverride {
	result := codexModelMetadataOverride{UpstreamModelMetadata: UpstreamModelMetadata{ID: strings.TrimSpace(modelID)}}
	result.CodexToolCapabilities = make(map[string]json.RawMessage)
	for _, field := range codexToolCapabilityFields {
		value := bytes.TrimSpace(candidates[0].CodexToolCapabilities[field])
		shared := len(value) > 0
		declared := shared
		for _, candidate := range candidates[1:] {
			declared = declared || len(candidate.CodexToolCapabilities[field]) > 0
			if !bytes.Equal(value, bytes.TrimSpace(candidate.CodexToolCapabilities[field])) {
				shared = false
			}
		}
		if shared {
			result.CodexToolCapabilities[field] = value
		} else if declared {
			fallback := json.RawMessage("null")
			if field == "supports_search_tool" || field == "use_responses_lite" {
				fallback = json.RawMessage("false")
			}
			result.CodexToolCapabilities[field] = fallback
		}
	}
	for _, candidate := range candidates {
		if result.DisplayName == "" && strings.TrimSpace(candidate.DisplayName) != "" {
			result.DisplayName = strings.TrimSpace(candidate.DisplayName)
		}
		if result.Description == "" && strings.TrimSpace(candidate.Description) != "" {
			result.Description = strings.TrimSpace(candidate.Description)
		}
	}

	reasoningKnown := true
	reasoningValue := false
	for i, candidate := range candidates {
		if candidate.Reasoning == nil {
			reasoningKnown = false
			break
		}
		if i == 0 {
			reasoningValue = *candidate.Reasoning
			continue
		}
		if reasoningValue != *candidate.Reasoning {
			reasoningKnown = false
			result.reasoningConflict = true
			break
		}
	}
	if reasoningKnown {
		result.Reasoning = &reasoningValue
		if reasoningValue {
			levels := normalizeReasoningLevels(candidates[0].SupportedReasoningLevels)
			for _, candidate := range candidates[1:] {
				levels = intersectOrderedStrings(levels, normalizeReasoningLevels(candidate.SupportedReasoningLevels))
			}
			result.SupportedReasoningLevels = levels
			if len(levels) == 0 {
				result.reasoningConflict = true
			} else {
				sharedDefault := normalizeReasoningLevel(candidates[0].DefaultReasoningLevel)
				for _, candidate := range candidates[1:] {
					if normalizeReasoningLevel(candidate.DefaultReasoningLevel) != sharedDefault {
						sharedDefault = ""
						break
					}
				}
				if !stringSliceContains(levels, sharedDefault) {
					sharedDefault = levels[0]
				}
				result.DefaultReasoningLevel = sharedDefault
			}
		}
	}

	modalitiesKnown := true
	modalities := normalizeCodexInputModalities(candidates[0].InputModalities)
	if len(modalities) == 0 {
		modalitiesKnown = false
	}
	for _, candidate := range candidates[1:] {
		candidateModalities := normalizeCodexInputModalities(candidate.InputModalities)
		if len(candidateModalities) == 0 {
			modalitiesKnown = false
			break
		}
		modalities = intersectOrderedStrings(modalities, candidateModalities)
	}
	if modalitiesKnown && len(modalities) > 0 {
		result.InputModalities = modalities
	} else if modalitiesKnown {
		result.inputModalitiesConflict = true
	}

	contextKnown := true
	for i, candidate := range candidates {
		if candidate.ContextWindow <= 0 {
			contextKnown = false
			break
		}
		if i == 0 || candidate.ContextWindow < result.ContextWindow {
			result.ContextWindow = candidate.ContextWindow
		}
	}
	if !contextKnown {
		result.ContextWindow = 0
	}
	return result
}

func applyUpstreamModelMetadataToCodexDescriptor(
	descriptor *configuredCodexModelDescriptor,
	metadata codexModelMetadataOverride,
) {
	if descriptor == nil {
		return
	}
	if strings.TrimSpace(metadata.DisplayName) != "" {
		descriptor.DisplayName = strings.TrimSpace(metadata.DisplayName)
	}
	if strings.TrimSpace(metadata.Description) != "" {
		descriptor.Description = strings.TrimSpace(metadata.Description)
	}
	if metadata.reasoningConflict {
		descriptor.DefaultReasoningLevel = nil
		descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{}
	} else if metadata.Reasoning != nil && !*metadata.Reasoning {
		none := "none"
		descriptor.DefaultReasoningLevel = &none
		descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{{
			Effort:      "none",
			Description: configuredCodexReasoningLevelDescription("none"),
		}}
	} else if metadata.Reasoning != nil && *metadata.Reasoning {
		levels := normalizeReasoningLevels(metadata.SupportedReasoningLevels)
		if len(levels) == 0 {
			descriptor.DefaultReasoningLevel = nil
			descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{}
		} else {
			defaultLevel := normalizeReasoningLevel(metadata.DefaultReasoningLevel)
			if !stringSliceContains(levels, defaultLevel) {
				defaultLevel = levels[0]
			}
			descriptor.DefaultReasoningLevel = &defaultLevel
			descriptor.SupportedReasoningLevels = make([]configuredCodexReasoningLevel, 0, len(levels))
			for _, level := range levels {
				descriptor.SupportedReasoningLevels = append(descriptor.SupportedReasoningLevels, configuredCodexReasoningLevel{
					Effort:      level,
					Description: configuredCodexReasoningLevelDescription(level),
				})
			}
		}
	}
	if metadata.inputModalitiesConflict {
		descriptor.InputModalities = []string{"text"}
	} else if modalities := normalizeCodexInputModalities(metadata.InputModalities); len(modalities) > 0 {
		descriptor.InputModalities = modalities
	}
	if metadata.ContextWindow > 0 {
		descriptor.ContextWindow = metadata.ContextWindow
		descriptor.MaxContextWindow = metadata.ContextWindow
	}
}

// applyOfficialCodexCatalogMetadataToManifest 将官方 OAuth 清单中的实时能力用于本地生成的模型项。
func applyOfficialCodexCatalogMetadataToManifest(
	body []byte,
	officialBody []byte,
	modelIDs []string,
	accounts []Account,
) ([]byte, bool, error) {
	officialMetadata, err := parseOfficialCodexCatalogMetadata(officialBody)
	if err != nil {
		return nil, false, fmt.Errorf("parse official Codex catalog metadata: %w", err)
	}
	if len(officialMetadata) == 0 || len(modelIDs) == 0 {
		return body, false, nil
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, fmt.Errorf("decode generated Codex manifest: %w", err)
	}
	var models []json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		return nil, false, fmt.Errorf("decode generated Codex models: %w", err)
	}
	metadataModels := codexCatalogMetadataModels(PlatformOpenAI, modelIDs, accounts, nil, true)
	changed := false
	for i, rawModel := range models {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawModel, &fields); err != nil {
			return nil, false, fmt.Errorf("decode generated Codex model: %w", err)
		}
		var slug string
		if len(fields["slug"]) == 0 || json.Unmarshal(fields["slug"], &slug) != nil {
			continue
		}
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		target := strings.TrimSpace(metadataModels[slug])
		if target == "" {
			target = slug
		}
		metadata, ok := officialCodexModelMetadataForID(officialMetadata, target)
		if !ok {
			continue
		}
		if err := applyOfficialCodexMetadataFields(fields, metadata); err != nil {
			return nil, false, fmt.Errorf("apply official metadata to Codex model %q: %w", slug, err)
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, false, fmt.Errorf("encode generated Codex model %q: %w", slug, err)
		}
		if !bytes.Equal(bytes.TrimSpace(encoded), bytes.TrimSpace(rawModel)) {
			models[i] = encoded
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}

	encodedModels, err := json.Marshal(models)
	if err != nil {
		return nil, false, fmt.Errorf("encode generated Codex models: %w", err)
	}
	envelope["models"] = encodedModels
	updated, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, fmt.Errorf("encode generated Codex manifest: %w", err)
	}
	return updated, true, nil
}

func applyOfficialCodexMetadataFields(fields map[string]json.RawMessage, metadata UpstreamModelMetadata) error {
	if metadata.Reasoning != nil {
		levels := normalizeReasoningLevels(metadata.SupportedReasoningLevels)
		defaultLevel := normalizeReasoningLevel(metadata.DefaultReasoningLevel)
		if !*metadata.Reasoning {
			levels = []string{"none"}
			defaultLevel = "none"
		} else if len(levels) > 0 {
			if !stringSliceContains(levels, defaultLevel) {
				defaultLevel = levels[0]
			}
		} else {
			defaultLevel = ""
		}
		if len(levels) > 0 {
			codexLevels := make([]configuredCodexReasoningLevel, 0, len(levels))
			for _, level := range levels {
				codexLevels = append(codexLevels, configuredCodexReasoningLevel{
					Effort:      level,
					Description: configuredCodexReasoningLevelDescription(level),
				})
			}
			rawLevels, err := json.Marshal(codexLevels)
			if err != nil {
				return fmt.Errorf("encode reasoning levels: %w", err)
			}
			fields["supported_reasoning_levels"] = rawLevels
		}
		if defaultLevel != "" {
			rawDefault, err := json.Marshal(defaultLevel)
			if err != nil {
				return fmt.Errorf("encode default reasoning level: %w", err)
			}
			fields["default_reasoning_level"] = rawDefault
		}
	}
	if modalities := normalizeCodexInputModalities(metadata.InputModalities); len(modalities) > 0 {
		rawModalities, err := json.Marshal(modalities)
		if err != nil {
			return fmt.Errorf("encode input modalities: %w", err)
		}
		fields["input_modalities"] = rawModalities
	}
	if metadata.ContextWindow > 0 {
		rawContextWindow, err := json.Marshal(metadata.ContextWindow)
		if err != nil {
			return fmt.Errorf("encode context window: %w", err)
		}
		fields["context_window"] = rawContextWindow
		fields["max_context_window"] = rawContextWindow
	}
	if metadata.MaxOutputTokens > 0 {
		rawMaxOutputTokens, err := json.Marshal(metadata.MaxOutputTokens)
		if err != nil {
			return fmt.Errorf("encode max output tokens: %w", err)
		}
		fields["max_output_tokens"] = rawMaxOutputTokens
	}
	applyCodexToolCapabilities(fields, metadata.CodexToolCapabilities, true)
	return nil
}

func parseOfficialCodexCatalogMetadata(body []byte) (map[string]UpstreamModelMetadata, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode official Codex manifest: %w", err)
	}
	modelsRaw, ok := envelope["models"]
	modelsRaw = bytes.TrimSpace(modelsRaw)
	if !ok || len(modelsRaw) == 0 || modelsRaw[0] != '[' {
		return nil, fmt.Errorf("official Codex manifest has no models array")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(modelsRaw, &entries); err != nil {
		return nil, fmt.Errorf("decode official Codex model entries: %w", err)
	}

	metadataByID := make(map[string]UpstreamModelMetadata, len(entries))
	seen := make(map[string]struct{}, len(entries))
	ambiguous := make(map[string]struct{})
	for _, raw := range entries {
		var entry struct {
			Slug                     string            `json:"slug"`
			Reasoning                *bool             `json:"reasoning"`
			DefaultReasoningLevel    string            `json:"default_reasoning_level"`
			SupportedReasoningLevels []json.RawMessage `json:"supported_reasoning_levels"`
			InputModalities          []string          `json:"input_modalities"`
			ContextWindow            int64             `json:"context_window"`
			MaxContextWindow         int64             `json:"max_context_window"`
			MaxOutputTokens          int64             `json:"max_output_tokens"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		modelID := strings.TrimSpace(entry.Slug)
		if modelID == "" {
			continue
		}
		if _, duplicate := seen[modelID]; duplicate {
			delete(metadataByID, modelID)
			ambiguous[modelID] = struct{}{}
			continue
		}
		seen[modelID] = struct{}{}
		if _, duplicate := ambiguous[modelID]; duplicate {
			continue
		}

		levels := reasoningLevelsFromRawEntries(entry.SupportedReasoningLevels)
		reasoning := entry.Reasoning
		if reasoning == nil && len(levels) > 0 {
			inferred := len(levels) != 1 || levels[0] != "none"
			reasoning = &inferred
		}
		defaultLevel := normalizeReasoningLevel(entry.DefaultReasoningLevel)
		if reasoning != nil && !*reasoning {
			levels = []string{"none"}
			defaultLevel = "none"
		} else if reasoning != nil && *reasoning && len(levels) == 0 {
			reasoning = nil
			defaultLevel = ""
		}
		if defaultLevel == "" && len(levels) > 0 {
			defaultLevel = levels[0]
		}
		contextWindow := entry.ContextWindow
		if contextWindow <= 0 {
			contextWindow = entry.MaxContextWindow
		}
		fields := make(map[string]json.RawMessage)
		if err := json.Unmarshal(raw, &fields); err != nil {
			continue
		}
		toolCapabilities := make(map[string]json.RawMessage)
		applyCodexToolCapabilities(toolCapabilities, fields, true)
		metadata := UpstreamModelMetadata{
			ID:                       modelID,
			Reasoning:                reasoning,
			DefaultReasoningLevel:    defaultLevel,
			SupportedReasoningLevels: levels,
			InputModalities:          normalizeCodexInputModalities(entry.InputModalities),
			ContextWindow:            contextWindow,
			MaxOutputTokens:          entry.MaxOutputTokens,
			CodexToolCapabilities:    toolCapabilities,
		}
		if upstreamModelMetadataIsUseful(metadata) {
			metadataByID[modelID] = metadata
		}
	}
	return metadataByID, nil
}

func officialCodexModelMetadataForID(
	metadataByID map[string]UpstreamModelMetadata,
	modelID string,
) (UpstreamModelMetadata, bool) {
	modelID = strings.TrimSpace(modelID)
	if metadata, ok := metadataByID[modelID]; ok {
		return metadata, true
	}
	target := strings.ToLower(codexProviderQualifiedModelID(modelID))
	if target == "" {
		return UpstreamModelMetadata{}, false
	}
	var matched UpstreamModelMetadata
	found := false
	for candidateID, metadata := range metadataByID {
		if strings.ToLower(codexProviderQualifiedModelID(candidateID)) != target {
			continue
		}
		if found {
			return UpstreamModelMetadata{}, false
		}
		matched = metadata
		found = true
	}
	return matched, found
}

func configuredCodexReasoningLevelDescription(level string) string {
	switch level {
	case "none":
		return "Use the model's default behavior without configurable reasoning"
	case "minimal":
		return "Minimal reasoning for the fastest responses"
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balanced reasoning for most coding tasks"
	case "high":
		return "Greater reasoning depth for coding and agent tasks"
	case "xhigh":
		return "Extra-high reasoning depth for difficult tasks"
	case "max":
		return "Maximum reasoning depth for complex tasks"
	case "ultra":
		return "Maximum reasoning with automatic task delegation"
	default:
		return "Reasoning effort supported by the upstream model"
	}
}

func intersectOrderedStrings(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	intersection := make([]string, 0, len(left))
	for _, value := range left {
		if _, ok := rightSet[value]; ok {
			intersection = append(intersection, value)
		}
	}
	return intersection
}

func stringSliceContains(values []string, target string) bool {
	if target == "" {
		return false
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
