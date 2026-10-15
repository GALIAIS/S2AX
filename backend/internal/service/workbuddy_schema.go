package service

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var workBuddyToolName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var workBuddyInvalidToolName = regexp.MustCompile(`[^A-Za-z0-9_-]`)
var workBuddyPromptRewrites = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)x-anthropic-billing-header:[^\n]*\n?`), ""},
	{regexp.MustCompile(`(?i)You are Claude Code, Anthropic['’]s official CLI for Claude`), "You are Claude Code, Anthropic's CLI for Claude"},
	{regexp.MustCompile(`(?i)Main branch \(you will usually use this for PRs\)`), "Main branch (usually the base for PRs)"},
	{regexp.MustCompile(`(?i)(the )?Codex CLI is an open source project led by OpenAI\.`), "The Codex CLI is an open source project led by OpenAI."},
}

func prepareWorkBuddyChat(chat map[string]any) (map[string]string, error) {
	messages, ok := chat["messages"].([]any)
	if !ok || len(messages) == 0 {
		return nil, errors.New("WorkBuddy requires chat messages")
	}
	unflag := func(text string) string {
		for _, rule := range workBuddyPromptRewrites {
			text = rule.pattern.ReplaceAllString(text, rule.replacement)
		}
		return text
	}
	out := make([]any, 0, len(messages)+1)
	for _, raw := range messages {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("invalid WorkBuddy chat message")
		}
		if m["role"] == "developer" {
			m["role"] = "system"
		}
		switch content := m["content"].(type) {
		case string:
			m["content"] = unflag(content)
			if m["role"] == "system" && strings.TrimSpace(m["content"].(string)) == "" {
				continue
			}
		case []any:
			parts := make([]any, 0, len(content))
			for _, rawPart := range content {
				part, _ := rawPart.(map[string]any)
				if text, ok := part["text"].(string); ok {
					text = unflag(text)
					if strings.TrimSpace(text) == "" {
						continue
					}
					part["text"] = text
				}
				parts = append(parts, rawPart)
			}
			m["content"] = parts
			if m["role"] == "system" && len(parts) == 0 {
				continue
			}
		}
		out = append(out, m)
	}
	if len(out) == 0 || out[0].(map[string]any)["role"] != "system" {
		out = append([]any{map[string]any{"role": "system", "content": "You are a helpful assistant."}}, out...)
	}
	chat["messages"] = out
	if maxTokens, exists := chat["max_completion_tokens"]; exists {
		if _, set := chat["max_tokens"]; !set {
			chat["max_tokens"] = maxTokens
		}
		delete(chat, "max_completion_tokens")
	}
	chat["stream"] = true
	options, _ := chat["stream_options"].(map[string]any)
	if options == nil {
		options = make(map[string]any)
	}
	options["include_usage"] = true
	chat["stream_options"] = options
	delete(chat, "prompt_cache_key")
	tools, _ := chat["tools"].([]any)
	functions := make([]map[string]any, 0, len(tools))
	for _, raw := range tools {
		t, _ := raw.(map[string]any)
		if fn, ok := t["function"].(map[string]any); ok {
			functions = append(functions, fn)
		}
	}
	legacy, _ := chat["functions"].([]any)
	for _, raw := range legacy {
		if fn, ok := raw.(map[string]any); ok {
			functions = append(functions, fn)
		}
	}
	taken := make(map[string]bool)
	theirs := make(map[string]string)
	names := make(map[string]string)
	for _, fn := range functions {
		name, _ := fn["name"].(string)
		if workBuddyToolName.MatchString(name) {
			taken[name] = true
		}
	}
	for _, fn := range functions {
		name, _ := fn["name"].(string)
		if !workBuddyToolName.MatchString(name) {
			safe := theirs[name]
			if safe == "" {
				base := workBuddyInvalidToolName.ReplaceAllString(name, "_")
				if base == "" {
					base = "tool"
				}
				safe = base
				for i := 2; taken[safe]; i++ {
					safe = base + "_" + strconv.Itoa(i)
				}
				taken[safe] = true
				theirs[name] = safe
				names[safe] = name
			}
			fn["name"] = safe
		}
		parameters, exists := fn["parameters"]
		if !exists {
			continue
		}
		shape, err := fitWorkBuddySchema(parameters, "", 0)
		if err != nil {
			return nil, err
		}
		root, ok := shape.(map[string]any)
		if !ok {
			root = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		workBuddyObjectRoot(root, root, 0)
		fn["parameters"] = root
	}
	rename := func(fn map[string]any) {
		if name := theirs[workBuddyString(fn["name"])]; name != "" {
			fn["name"] = name
		}
	}
	for _, raw := range out {
		m := raw.(map[string]any)
		calls, _ := m["tool_calls"].([]any)
		for _, rawCall := range calls {
			call, _ := rawCall.(map[string]any)
			fn, _ := call["function"].(map[string]any)
			rename(fn)
		}
		if m["role"] == "tool" || m["role"] == "function" {
			rename(m)
		}
		if fn, ok := m["function_call"].(map[string]any); ok {
			rename(fn)
		}
	}
	if choice, ok := chat["tool_choice"].(map[string]any); ok {
		kind := workBuddyString(choice["type"])
		switch kind {
		case "auto", "required", "none":
			chat["tool_choice"] = kind
		case "function":
			fn, _ := choice["function"].(map[string]any)
			rename(fn)
			name := workBuddyString(fn["name"])
			if name == "" {
				name = "auto"
			}
			chat["tool_choice"] = name
		default:
			delete(chat, "tool_choice")
		}
	}
	if choice, ok := chat["tool_choice"].(string); ok {
		if safe := theirs[choice]; safe != "" {
			chat["tool_choice"] = safe
		}
		if strings.EqualFold(strings.TrimSpace(choice), "none") {
			delete(chat, "tool_choice")
			delete(chat, "tools")
			delete(chat, "functions")
		}
	}
	if fn, ok := chat["function_call"].(map[string]any); ok {
		rename(fn)
	}
	return names, nil
}

var workBuddySchemaTypes = []string{"string", "number", "integer", "boolean", "object", "array", "null"}

func fitWorkBuddySchema(raw any, keyword string, depth int) (any, error) {
	if depth > 64 {
		return nil, errors.New("WorkBuddy tool schema exceeds 64 levels")
	}
	if b, ok := raw.(bool); ok && keyword != "additionalProperties" && keyword != "unevaluatedProperties" {
		if b {
			return map[string]any{}, nil
		}
		return map[string]any{"not": map[string]any{}}, nil
	}
	s, ok := raw.(map[string]any)
	if !ok {
		return raw, nil
	}
	if pattern, ok := s["pattern"].(string); ok {
		var out strings.Builder
		for i := 0; i < len(pattern); i++ {
			if pattern[i] == '\\' && i+1 < len(pattern) {
				if pattern[i+1] == '\\' {
					_, _ = out.WriteString(pattern[i : i+2])
					i++
					continue
				}
				if pattern[i+1] == '0' && (i+2 == len(pattern) || pattern[i+2] < '0' || pattern[i+2] > '9') {
					_, _ = out.WriteString(`\u0000`)
					i++
					continue
				}
			}
			_ = out.WriteByte(pattern[i])
		}
		s["pattern"] = out.String()
	}
	if tuple, ok := s["items"].([]any); ok {
		if len(tuple) == 1 {
			s["items"] = tuple[0]
		} else if len(tuple) > 1 {
			s["items"] = map[string]any{"anyOf": tuple}
		} else {
			s["items"] = map[string]any{}
		}
		delete(s, "additionalItems")
	}
	if rawType, exists := s["type"]; exists {
		list, ok := rawType.([]any)
		if !ok {
			list = []any{rawType}
		}
		types := []any{}
		for _, raw := range list {
			t := strings.ToLower(workBuddyString(raw))
			if slices.Contains(workBuddySchemaTypes, t) && !slices.Contains(types, any(t)) {
				types = append(types, t)
			}
		}
		switch len(types) {
		case 0:
			delete(s, "type")
		case 1:
			s["type"] = types[0]
		default:
			s["type"] = types
		}
	}
	if required, exists := s["required"]; exists {
		if list, ok := required.([]any); ok {
			names := []any{}
			for _, item := range list {
				if name, ok := item.(string); ok {
					names = append(names, name)
				}
			}
			s["required"] = names
		} else {
			delete(s, "required")
		}
	}
	fitWorkBuddyEnum(s)
	for _, key := range []string{"items", "additionalItems", "unevaluatedItems", "contains", "not", "if", "then", "else", "propertyNames", "additionalProperties", "unevaluatedProperties"} {
		if value, exists := s[key]; exists {
			fitted, err := fitWorkBuddySchema(value, key, depth+1)
			if err != nil {
				return nil, err
			}
			s[key] = fitted
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		list, _ := s[key].([]any)
		for i, value := range list {
			fitted, err := fitWorkBuddySchema(value, key, depth+1)
			if err != nil {
				return nil, err
			}
			list[i] = fitted
		}
	}
	for _, key := range []string{"properties", "patternProperties", "$defs", "definitions", "dependentSchemas"} {
		values, _ := s[key].(map[string]any)
		for name, value := range values {
			fitted, err := fitWorkBuddySchema(value, key, depth+1)
			if err != nil {
				return nil, err
			}
			values[name] = fitted
		}
	}
	return s, nil
}

func fitWorkBuddyEnum(s map[string]any) {
	raw, exists := s["enum"]
	if !exists {
		return
	}
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		delete(s, "enum")
		return
	}
	listed, ok := s["type"].([]any)
	if !ok {
		listed = []any{s["type"]}
	}
	count := 0
	for _, t := range listed {
		if t != nil && t != "null" {
			count++
		}
	}
	if count == 1 {
		return
	}
	groups := make(map[string][]any)
	order := []string{}
	for _, value := range values {
		kind := "null"
		switch v := value.(type) {
		case string:
			kind = "string"
		case bool:
			kind = "boolean"
		case float64:
			kind = "number"
			if math.Trunc(v) == v {
				kind = "integer"
			}
		case []any:
			kind = "array"
		case map[string]any:
			kind = "object"
		}
		if _, exists := groups[kind]; !exists {
			order = append(order, kind)
		}
		groups[kind] = append(groups[kind], value)
	}
	if groups["integer"] != nil && groups["number"] != nil {
		groups["number"] = append(groups["number"], groups["integer"]...)
		delete(groups, "integer")
		order = slices.DeleteFunc(order, func(k string) bool { return k == "integer" })
	}
	hasNull := groups["null"] != nil
	delete(groups, "null")
	order = slices.DeleteFunc(order, func(k string) bool { return k == "null" })
	if len(order) <= 1 {
		if len(order) == 0 {
			s["type"] = "null"
		} else if hasNull {
			s["type"] = []any{order[0], "null"}
		} else {
			s["type"] = order[0]
		}
		return
	}
	branches := []any{}
	for _, kind := range order {
		branches = append(branches, map[string]any{"type": kind, "enum": groups[kind]})
	}
	if hasNull {
		branches = append(branches, map[string]any{"type": "null"})
	}
	delete(s, "enum")
	delete(s, "type")
	if _, ok := s["anyOf"].([]any); ok {
		all, _ := s["allOf"].([]any)
		s["allOf"] = append(all, map[string]any{"anyOf": branches})
	} else {
		s["anyOf"] = branches
	}
}

func workBuddyObjectRoot(s, root map[string]any, depth int) {
	if depth > 8 {
		return
	}
	if ref, ok := s["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
		var target any = root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, _ := target.(map[string]any)
			target = m[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
		}
		if m, ok := target.(map[string]any); ok && !reflect.DeepEqual(m, s) {
			for k, v := range m {
				if _, exists := s[k]; !exists {
					s[k] = v
				}
			}
			delete(s, "$ref")
		}
	}
	props, _ := s["properties"].(map[string]any)
	if props == nil {
		props = make(map[string]any)
	}
	required, _ := s["required"].([]any)
	branches := []map[string]any{}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		list, _ := s[key].([]any)
		for _, raw := range list {
			branch, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			encoded, _ := json.Marshal(branch)
			b := make(map[string]any)
			if json.Unmarshal(encoded, &b) != nil {
				continue
			}
			workBuddyObjectRoot(b, root, depth+1)
			bp, _ := b["properties"].(map[string]any)
			br, _ := b["required"].([]any)
			if key == "allOf" {
				for name, value := range bp {
					if _, exists := props[name]; !exists {
						props[name] = value
					}
				}
				required = append(required, br...)
			} else {
				branches = append(branches, b)
			}
		}
		delete(s, key)
	}
	variants := make(map[string][]any)
	for _, b := range branches {
		bp, _ := b["properties"].(map[string]any)
		for key, value := range bp {
			if _, exists := props[key]; exists {
				continue
			}
			if !slices.ContainsFunc(variants[key], func(old any) bool { return reflect.DeepEqual(old, value) }) {
				variants[key] = append(variants[key], value)
			}
		}
	}
	for key, list := range variants {
		if len(list) == 1 {
			props[key] = list[0]
		} else {
			props[key] = map[string]any{"anyOf": list}
		}
	}
	if len(branches) > 0 {
		first, _ := branches[0]["required"].([]any)
		for _, raw := range first {
			name, ok := raw.(string)
			if !ok {
				continue
			}
			every := true
			for _, b := range branches {
				req, _ := b["required"].([]any)
				if !slices.Contains(req, any(name)) {
					every = false
					break
				}
			}
			if every {
				required = append(required, name)
			}
		}
	}
	unique := []any{}
	for _, raw := range required {
		if name, ok := raw.(string); ok {
			if _, exists := props[name]; exists && !slices.Contains(unique, any(name)) {
				unique = append(unique, name)
			}
		}
	}
	s["type"] = "object"
	s["properties"] = props
	if len(unique) > 0 {
		s["required"] = unique
	} else {
		delete(s, "required")
	}
}
