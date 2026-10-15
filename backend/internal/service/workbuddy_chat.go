package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 会话哈希使用实例共享的 JWT 密钥并包含 API Key ID，不透传客户端自报的 WorkBuddy ID。
func workBuddyConversationHeaders(c *gin.Context, chat map[string]any, secret string) http.Header {
	session := ""
	tenant := int64(0)
	if c != nil {
		tenant = getAPIKeyIDFromContext(c)
		for _, key := range []string{"x-magpie-workbuddy-session", "session_id", "x-session-id", "x-conversation-id"} {
			if session = strings.TrimSpace(c.GetHeader(key)); session != "" {
				break
			}
		}
		if session == "" {
			session = resolveOpenCodeSessionID(c, nil, false, openCodeInboundBodies(c)...)
		}
	}
	if session == "" {
		session = workBuddyString(chat["prompt_cache_key"])
	}
	if len(session) > 256 {
		session = session[:256]
	}
	messages, _ := chat["messages"].([]any)
	conversation, turn := "", ""
	if session != "" {
		conversation = "session:" + session
	}
	for i, raw := range messages {
		m, _ := raw.(map[string]any)
		if m["role"] != "user" {
			continue
		}
		sig := workBuddySignature(m["content"])
		if sig == "" {
			continue
		}
		if conversation == "" {
			conversation = "first:" + sig
		}
		if i > 0 {
			prev, _ := messages[i-1].(map[string]any)
			calls, _ := prev["tool_calls"].([]any)
			if prev["role"] == "tool" || (prev["role"] == "assistant" && len(calls) > 0) {
				continue
			}
		}
		turn = strconv.Itoa(i) + ":" + sig
	}
	derive := func(kind, key string) string {
		h := hmac.New(sha256.New, []byte(secret))
		fmt.Fprintf(h, "workbuddy|%d|%s|%s", tenant, kind, key)
		return hex.EncodeToString(h.Sum(nil))[:32]
	}
	h := http.Header{}
	if conversation != "" {
		h.Set("X-Conversation-ID", derive("conversation", conversation))
	}
	if turn != "" {
		h.Set("X-Conversation-Request-ID", derive("turn", conversation+"|"+turn))
	} else {
		h.Set("X-Conversation-Request-ID", workBuddyID())
	}
	return h
}

func workBuddySignature(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts, _ := content.([]any)
	var out strings.Builder
	for _, raw := range parts {
		p, _ := raw.(map[string]any)
		kind := workBuddyString(p["type"])
		if kind == "tool_result" || kind == "tool-result" {
			continue
		}
		if kind == "text" || kind == "" {
			if s, ok := p["text"].(string); ok {
				_, _ = out.WriteString(s)
			}
			continue
		}
		data, _ := json.Marshal(p)
		digest := sha256.Sum256(data)
		_, _ = out.WriteString(kind + ":" + hex.EncodeToString(digest[:]))
	}
	return out.String()
}

func (s *WorkBuddyService) Chat(ctx context.Context, c *gin.Context, account *Account, body []byte, stream bool, secret string) (*http.Response, error) {
	if secret == "" {
		return nil, errors.New("WorkBuddy session isolation requires a configured JWT secret")
	}
	a, err := s.FreshAccount(ctx, account)
	if err != nil {
		return nil, err
	}
	var chat map[string]any
	if err := json.Unmarshal(body, &chat); err != nil || chat == nil {
		return nil, errors.New("invalid WorkBuddy chat request")
	}
	ids := workBuddyConversationHeaders(c, chat, secret)
	names, err := prepareWorkBuddyChat(chat)
	if err != nil {
		return nil, err
	}
	endpoint, err := workBuddyEndpoint(a.Platform)
	if err != nil {
		return nil, err
	}
	proxy, err := s.proxyURL(ctx, a.ProxyID)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		payload, err := json.Marshal(chat)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v2/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header = workBuddyHeaders(a.Platform, a.Credentials)
		req.Header.Set("Accept", "text/event-stream")
		for key, values := range ids {
			req.Header[key] = values
		}
		id := workBuddyID()
		req.Header.Set("X-Request-ID", id)
		req.Header.Set("X-Conversation-Message-ID", id)
		for key, value := range map[string]string{"X-Agent-Intent": "craft", "X-Agent-Purpose": "conversation", "X-IDE-Name": "WorkBuddy", "X-IDE-Version": workBuddyVersion} {
			req.Header.Set(key, value)
		}
		if a.Platform == PlatformWorkBuddyAI {
			req.Header.Set("X-Agent-Type", "main")
		}
		resp, err := s.http.Do(req, proxy, a.ID, a.Concurrency)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("WorkBuddy chat connection failed")
		}
		if resp.StatusCode >= 300 || !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			raw, readErr := io.ReadAll(io.LimitReader(resp.Body, workBuddyBodyLimit+1))
			closeWorkBuddyBody(resp.Body)
			if readErr != nil || len(raw) > workBuddyBodyLimit {
				return nil, errors.New("failed to read WorkBuddy chat response")
			}
			code, message := workBuddyErrorFields(raw)
			if attempt == 0 && a.Platform == PlatformWorkBuddyAI && (code == 11153 || (resp.StatusCode == 400 && strings.Contains(strings.ToLower(message), "must end with a user message"))) && repairWorkBuddyToolOrder(chat) {
				continue
			}
			return workBuddyErrorResponse(resp, raw), nil
		}
		wrapped := newWorkBuddyStream(resp.Body, names)
		resp.Body = wrapped
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
		resp.Header.Del("Content-Encoding")
		if stream {
			return resp, nil
		}
		aggregated, err := aggregateWorkBuddyStream(wrapped)
		closeWorkBuddyBody(wrapped)
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(aggregated))
		resp.ContentLength = int64(len(aggregated))
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	}
	return nil, errors.New("WorkBuddy chat retry exhausted")
}

func repairWorkBuddyToolOrder(chat map[string]any) bool {
	messages, _ := chat["messages"].([]any)
	out := make([]any, 0, len(messages)+1)
	changed := false
	for i, raw := range messages {
		m, _ := raw.(map[string]any)
		if i > 0 && m["role"] == "user" {
			prev, _ := messages[i-1].(map[string]any)
			if prev["role"] == "tool" {
				out = append(out, map[string]any{"role": "assistant", "content": ""})
				changed = true
			}
		}
		out = append(out, raw)
	}
	chat["messages"] = out
	return changed
}

func workBuddyErrorFields(raw []byte) (int, string) {
	var data map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&data) != nil {
		return 0, ""
	}
	if nested, ok := data["error"].(map[string]any); ok {
		data = nested
	}
	code, _ := strconv.Atoi(workBuddyString(data["code"]))
	message := workBuddyString(data["msg"])
	if message == "" {
		message = workBuddyString(data["message"])
	}
	return code, message
}

func workBuddyErrorResponse(resp *http.Response, raw []byte) *http.Response {
	code, message := workBuddyErrorFields(raw)
	status := resp.StatusCode
	if status < 400 {
		status = http.StatusBadGateway
	}
	kind := "api_error"
	safe := (&workBuddyAPIError{Code: code, Status: status}).Error()
	switch {
	case code == 14018:
		status = 429
		kind = "insufficient_quota"
	case code == 11140:
		status = 403
		kind = "permission_error"
	case strings.Contains(strings.ToLower(message), "unapproved channel") || strings.Contains(strings.ToLower(message), "illegal api invocation"):
		status = 403
		kind = "permission_error"
		safe = "WorkBuddy refused this client channel; use an authorized compatible WorkBuddy client"
	case status == 401:
		kind = "authentication_error"
	case status == 403:
		kind = "permission_error"
	case status == 400:
		kind = "invalid_request_error"
	}
	data, _ := json.Marshal(map[string]any{"error": map[string]any{"type": kind, "message": safe, "code": code}})
	resp.StatusCode = status
	resp.Status = http.StatusText(status)
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Del("Content-Length")
	resp.Header.Del("Content-Encoding")
	return resp
}

type workBuddyStream struct {
	source  io.ReadCloser
	scanner *bufio.Scanner
	names   map[string]string
	buffer  []byte
	err     error
	done    bool
}

func newWorkBuddyStream(body io.ReadCloser, names map[string]string) *workBuddyStream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), workBuddyBodyLimit)
	return &workBuddyStream{source: body, scanner: scanner, names: names}
}

func (s *workBuddyStream) Read(p []byte) (int, error) {
	for len(s.buffer) == 0 && !s.done {
		if !s.scanner.Scan() {
			s.err = s.scanner.Err()
			if s.err == nil {
				s.err = io.ErrUnexpectedEOF
			}
			s.done = true
			break
		}
		line := s.scanner.Text()
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				s.done = true
				s.err = io.EOF
				line += "\n"
			} else if payload != "" {
				var data map[string]any
				if json.Unmarshal([]byte(payload), &data) != nil {
					s.done = true
					s.err = errors.New("invalid WorkBuddy SSE data")
					break
				}
				if _, exists := data["error"]; exists {
					s.done = true
					s.err = errors.New("WorkBuddy stream returned an upstream error")
					break
				}
				if code, _ := workBuddyErrorFields([]byte(payload)); code != 0 {
					s.done = true
					s.err = (&workBuddyAPIError{Code: code, Status: 502})
					break
				}
				workBuddyRestoreNames(data, s.names)
				encoded, err := json.Marshal(data)
				if err != nil {
					s.done = true
					s.err = err
					break
				}
				line = "data: " + string(encoded)
			}
		}
		s.buffer = []byte(line + "\n")
	}
	if len(s.buffer) > 0 {
		n := copy(p, s.buffer)
		s.buffer = s.buffer[n:]
		return n, nil
	}
	return 0, s.err
}

func (s *workBuddyStream) Close() error {
	s.done = true
	s.buffer = nil
	if s.err == nil {
		s.err = io.EOF
	}
	return s.source.Close()
}

func workBuddyRestoreNames(data map[string]any, names map[string]string) {
	choices, _ := data["choices"].([]any)
	for _, raw := range choices {
		choice, _ := raw.(map[string]any)
		for _, key := range []string{"message", "delta"} {
			m, _ := choice[key].(map[string]any)
			calls, _ := m["tool_calls"].([]any)
			for _, rawCall := range calls {
				call, _ := rawCall.(map[string]any)
				fn, _ := call["function"].(map[string]any)
				if name := names[workBuddyString(fn["name"])]; name != "" {
					fn["name"] = name
				}
			}
			if fn, ok := m["function_call"].(map[string]any); ok {
				if name := names[workBuddyString(fn["name"])]; name != "" {
					fn["name"] = name
				}
			}
		}
	}
}

func aggregateWorkBuddyStream(body io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(body, 64<<20))
	scanner.Buffer(make([]byte, 64*1024), workBuddyBodyLimit)
	out := map[string]any{"object": "chat.completion"}
	choices := make(map[int]map[string]any)
	tools := make(map[int]map[int]map[string]any)
	done := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		if data == "" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, errors.New("invalid WorkBuddy stream chunk")
		}
		for _, key := range []string{"id", "model", "created", "usage", "service_tier", "system_fingerprint"} {
			if v, ok := chunk[key]; ok && v != nil {
				out[key] = v
			}
		}
		list, _ := chunk["choices"].([]any)
		for _, raw := range list {
			c, _ := raw.(map[string]any)
			index := 0
			if n, ok := c["index"].(float64); ok {
				index = int(n)
			}
			if choices[index] == nil {
				choices[index] = map[string]any{"index": index, "message": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}
				tools[index] = make(map[int]map[string]any)
			}
			choice := choices[index]
			m := choice["message"].(map[string]any)
			delta, _ := c["delta"].(map[string]any)
			for _, key := range []string{"content", "reasoning_content", "reasoning", "refusal"} {
				if text, ok := delta[key].(string); ok {
					old, _ := m[key].(string)
					m[key] = old + text
				}
			}
			if reason := c["finish_reason"]; reason != nil {
				choice["finish_reason"] = reason
			}
			if fn, ok := delta["function_call"].(map[string]any); ok {
				target, _ := m["function_call"].(map[string]any)
				if target == nil {
					target = map[string]any{"name": "", "arguments": ""}
					m["function_call"] = target
				}
				for _, key := range []string{"name", "arguments"} {
					if text, ok := fn[key].(string); ok {
						old, _ := target[key].(string)
						target[key] = old + text
					}
				}
			}
			calls, _ := delta["tool_calls"].([]any)
			for _, rawCall := range calls {
				call, _ := rawCall.(map[string]any)
				ti := 0
				if n, ok := call["index"].(float64); ok {
					ti = int(n)
				}
				if tools[index][ti] == nil {
					tools[index][ti] = map[string]any{"type": "function", "function": map[string]any{"name": "", "arguments": ""}}
				}
				target := tools[index][ti]
				if id := call["id"]; id != nil {
					target["id"] = id
				}
				fn, _ := call["function"].(map[string]any)
				targetFn := target["function"].(map[string]any)
				for _, key := range []string{"name", "arguments"} {
					if text, ok := fn[key].(string); ok {
						targetFn[key] = targetFn[key].(string) + text
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !done {
		return nil, errors.New("WorkBuddy stream ended before completion")
	}
	indexes := make([]int, 0, len(choices))
	for index := range choices {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	list := make([]any, 0, len(indexes))
	for _, index := range indexes {
		choice := choices[index]
		m := choice["message"].(map[string]any)
		if len(tools[index]) > 0 {
			keys := make([]int, 0, len(tools[index]))
			for k := range tools[index] {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			calls := make([]any, 0, len(keys))
			for _, k := range keys {
				calls = append(calls, tools[index][k])
			}
			m["tool_calls"] = calls
			if m["content"] == "" {
				m["content"] = nil
			}
		}
		list = append(list, choice)
	}
	if len(list) == 0 {
		return nil, errors.New("WorkBuddy stream contains no choices")
	}
	out["choices"] = list
	return json.Marshal(out)
}
