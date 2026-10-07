package openai

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const DecisionsModel = "gpt-6-luna"

// ValidateDecisionsRequest 保留原始请求；拒绝会让网关与上游解析不一致的重复字段。
func ValidateDecisionsRequest(body []byte) error {
	root := gjson.ParseBytes(body)
	if !gjson.ValidBytes(body) || !root.IsObject() {
		return fmt.Errorf("request must be a JSON object")
	}
	if err := validateDecisionsKeys(root, 0); err != nil {
		return err
	}
	if root.Get("model").Type != gjson.String || strings.TrimSpace(root.Get("model").String()) == "" {
		return fmt.Errorf("model must be a non-empty string")
	}
	if root.Get("stream").Exists() {
		return fmt.Errorf("Decisions does not support streaming")
	}
	var unsupported bool
	root.ForEach(func(key, _ gjson.Result) bool {
		switch key.String() {
		case "model", "input", "questions", "safety_identifier":
			return true
		default:
			unsupported = true
			return false
		}
	})
	if unsupported {
		return fmt.Errorf("Decisions accepts only model, input, questions, and safety_identifier")
	}
	if id := root.Get("safety_identifier"); id.Exists() && id.Type != gjson.Null && (id.Type != gjson.String || utf8.RuneCountInString(id.String()) > 128) {
		return fmt.Errorf("safety_identifier must be null or a string of at most 128 characters")
	}
	input := root.Get("input")
	if input.Type != gjson.String {
		if !input.IsArray() || len(input.Array()) == 0 {
			return fmt.Errorf("input must be a string or a non-empty array of user messages")
		}
		images := 0
		for _, message := range input.Array() {
			if !message.IsObject() || message.Get("role").String() != "user" {
				return fmt.Errorf("input supports only user messages")
			}
			if typ := message.Get("type"); typ.Exists() && typ.String() != "message" {
				return fmt.Errorf("input message type must be message")
			}
			content := message.Get("content")
			if content.Type == gjson.String {
				continue
			}
			if !content.IsArray() || len(content.Array()) == 0 {
				return fmt.Errorf("message content must be a string or non-empty array")
			}
			for _, part := range content.Array() {
				switch part.Get("type").String() {
				case "input_text":
					if part.Get("text").Type != gjson.String {
						return fmt.Errorf("input_text requires a text string")
					}
				case "input_image":
					images++
					if images > 128 {
						return fmt.Errorf("Decisions allows at most 128 images")
					}
					url := part.Get("image_url")
					meta, data, found := strings.Cut(url.String(), ",")
					if url.Type != gjson.String || !found || !strings.HasPrefix(meta, "data:image/") || !strings.HasSuffix(meta, ";base64") || data == "" || part.Get("file_id").Exists() {
						return fmt.Errorf("input_image requires an inline base64 image data URL; external URLs and file IDs are unsupported")
					}
					if _, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))); err != nil {
						return fmt.Errorf("input_image contains invalid base64 data")
					}
				default:
					return fmt.Errorf("Decisions supports only input_text and input_image parts")
				}
			}
		}
	}
	questions := root.Get("questions")
	if !questions.IsArray() || len(questions.Array()) == 0 {
		return fmt.Errorf("questions must be a non-empty array")
	}
	names := make(map[string]bool)
	for i, question := range questions.Array() {
		if !question.IsObject() || question.Get("instructions").Type != gjson.String {
			return fmt.Errorf("questions[%d] requires an instructions string", i)
		}
		if name := question.Get("name"); name.Exists() {
			if name.Type != gjson.String || names[name.String()] {
				return fmt.Errorf("question names must be unique strings when supplied")
			}
			names[name.String()] = true
		}
		var field string
		switch question.Get("type").String() {
		case "predicate":
			continue
		case "choice":
			field = "choices"
		case "score":
			field = "levels"
		default:
			return fmt.Errorf("questions[%d].type must be predicate, choice, or score", i)
		}
		options := question.Get(field)
		if !options.IsArray() || len(options.Array()) == 0 {
			return fmt.Errorf("questions[%d].%s must be a non-empty array", i, field)
		}
		values := make(map[string]bool)
		for _, option := range options.Array() {
			if !option.IsObject() {
				return fmt.Errorf("%s entries must be objects", field)
			}
			if description := option.Get("description"); description.Exists() && description.Type != gjson.String {
				return fmt.Errorf("option description must be a string")
			}
			if field == "levels" {
				if option.Get("label").Type != gjson.String {
					return fmt.Errorf("score levels require string labels")
				}
				continue
			}
			value := option.Get("value")
			if value.Type != gjson.String && value.Type != gjson.True && value.Type != gjson.False {
				return fmt.Errorf("choice values must be strings or booleans")
			}
			key := fmt.Sprintf("%d:%s", value.Type, value.String())
			if values[key] {
				return fmt.Errorf("choice values must be unique")
			}
			values[key] = true
		}
	}
	return nil
}

func validateDecisionsKeys(value gjson.Result, depth int) error {
	if depth > 64 {
		return fmt.Errorf("Decisions JSON nesting is too deep")
	}
	if !value.IsObject() && !value.IsArray() {
		return nil
	}
	seen := make(map[string]bool)
	var result error
	value.ForEach(func(key, child gjson.Result) bool {
		if value.IsObject() {
			if seen[key.String()] {
				result = fmt.Errorf("Decisions JSON contains duplicate fields")
				return false
			}
			seen[key.String()] = true
		}
		result = validateDecisionsKeys(child, depth+1)
		return result == nil
	})
	return result
}
