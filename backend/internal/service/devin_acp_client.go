package service

// Devin Cloud ACP (Agent Client Protocol, JSON-RPC 2.0 over WebSocket) 客户端。
//
// 协议事实（逆向自 Devin Desktop 3.10.x 的 windsurf 扩展并实测验证）：
//   - 端点: wss://<webappHost>/api/acp/live?token=<jwt>&x-cog-org-id=<org>
//     token 为 devin-session-token$ 去掉前缀后的 JWT；x-cog-org-id 可省略
//   - 握手序列: initialize -> session/new -> session/set_config_option ->
//     session/prompt；结果经 session/update 通知增量推送
//   - agent->client 请求（fs/terminal/permission/elicitation）必须应答，
//     否则 agent 会永久挂起
//   - session/prompt 结果: {stopReason, usage:{inputTokens,outputTokens,totalTokens}}

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coderws "github.com/coder/websocket"
)

const (
	devinACPProtocolVersion = 1
	devinACPReadLimitBytes  = 8 << 20 // 8MiB，单帧上限（远端可能推送大 chunk）
	devinACPDialTimeout     = 30 * time.Second
	devinACPPromptTimeout   = 20 * time.Minute
	devinACPCloseTimeout    = 5 * time.Second
)

// devinACPEvent 是 session/update 通知里对客户端有意义的增量事件。
type devinACPEvent struct {
	// Kind: "message"（agent_message_chunk）| "thought"（agent_thought_chunk）
	Kind string
	Text string
}

// devinACPTurnResult 是 session/prompt 的终态结果。
type devinACPTurnResult struct {
	StopReason    string
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	SessionID     string
	UserMessageID string
}

type devinRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *devinRPCError) Error() string {
	return fmt.Sprintf("acp error %d: %s", e.Code, e.Message)
}

// devinACPClient 管理一条 ACP WebSocket 连接上的 JSON-RPC 会话。
// 每条连接可并发承载多个 session，但本实现一次只跑一个 turn（请求级短连接）。
type devinACPClient struct {
	conn   *coderws.Conn
	nextID atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan devinRPCResult

	onEvent     func(sessionID string, ev devinACPEvent)
	autoApprove bool
	closed      chan struct{}
	closeOnce   sync.Once
	pumpErr     atomic.Value // error
}

type devinRPCResult struct {
	Result json.RawMessage
	Err    *devinRPCError
}

type devinRPCMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *devinRPCError  `json:"error,omitempty"`
}

// devinDialACP 建立到 Devin Cloud ACP 的 WebSocket 连接并完成 initialize。
func devinDialACP(ctx context.Context, account *Account, token, proxyURL string) (*devinACPClient, error) {
	jwt := strings.TrimPrefix(strings.TrimSpace(token), DevinSessionTokenPrefix)
	if jwt == "" {
		return nil, errors.New("devin session token is empty")
	}
	q := url.Values{}
	q.Set("token", jwt)
	if org := account.DevinOrgID(); org != "" {
		q.Set("x-cog-org-id", org)
	}
	wsURL := "wss://" + account.DevinWebappHost() + DevinACPPath + "?" + q.Encode()

	opts := &coderws.DialOptions{}
	if proxy := strings.TrimSpace(proxyURL); proxy != "" {
		parsed, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url: %w", err)
		}
		opts.HTTPClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(parsed)}}
	}

	dialCtx, cancel := context.WithTimeout(ctx, devinACPDialTimeout)
	defer cancel()
	conn, resp, err := coderws.Dial(dialCtx, wsURL, opts)
	if err != nil {
		status := 0
		var body []byte
		if resp != nil {
			status = resp.StatusCode
			if resp.Body != nil {
				body, _ = io.ReadAll(io.LimitReader(resp.Body, 4<<10))
				_ = resp.Body.Close()
			}
		}
		return nil, &UpstreamFailoverError{
			StatusCode:             statusOr(status, http.StatusBadGateway),
			ResponseBody:           body,
			RequestScopedTransient: true,
		}
	}
	conn.SetReadLimit(devinACPReadLimitBytes)

	cl := &devinACPClient{
		conn:        conn,
		pending:     make(map[int64]chan devinRPCResult),
		autoApprove: account.DevinAutoApprove(),
		closed:      make(chan struct{}),
	}
	go cl.readPump()

	if _, err := cl.call(ctx, "initialize", map[string]any{
		"protocolVersion": devinACPProtocolVersion,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
		"clientInfo": map[string]any{"name": "sub2api", "version": "1.0.0"},
	}); err != nil {
		cl.Close()
		return nil, fmt.Errorf("devin acp initialize: %w", err)
	}
	return cl, nil
}

func statusOr(v, dflt int) int {
	if v <= 0 {
		return dflt
	}
	return v
}

// Close 关闭 WS 连接；幂等。
func (cl *devinACPClient) Close() {
	cl.closeOnce.Do(func() {
		close(cl.closed)
		_ = cl.conn.Close(coderws.StatusNormalClosure, "")
		cl.pendingMu.Lock()
		for id, ch := range cl.pending {
			delete(cl.pending, id)
			ch <- devinRPCResult{Err: &devinRPCError{Code: -32000, Message: "connection closed"}}
		}
		cl.pendingMu.Unlock()
	})
}

// call 发送 JSON-RPC 请求并等待响应。
func (cl *devinACPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	id := cl.nextID.Add(1)
	ch := make(chan devinRPCResult, 1)
	cl.pendingMu.Lock()
	cl.pending[id] = ch
	cl.pendingMu.Unlock()
	defer func() {
		cl.pendingMu.Lock()
		delete(cl.pending, id)
		cl.pendingMu.Unlock()
	}()

	frame, err := json.Marshal(devinRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		return nil, err
	}
	if err := cl.conn.Write(ctx, coderws.MessageText, frame); err != nil {
		return nil, err
	}
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-cl.closed:
		if err := cl.pumpErr.Load(); err != nil {
			return nil, err.(error)
		}
		return nil, errors.New("devin acp connection closed")
	}
}

// readPump 读循环：响应派发给 pending；session/update 通知回调 onEvent；
// agent->client 请求自动应答（fs/terminal 一律 method_not_found，permission 按配置应答）。
func (cl *devinACPClient) readPump() {
	for {
		msgType, data, err := cl.conn.Read(context.Background())
		if err != nil {
			cl.pumpErr.Store(err)
			cl.Close()
			return
		}
		if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
			continue
		}
		var m devinRPCMessage
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		switch {
		case len(m.ID) > 0 && (m.Result != nil || m.Error != nil):
			// 对我们请求的响应
			var idNum int64
			if json.Unmarshal(m.ID, &idNum) == nil {
				cl.pendingMu.Lock()
				ch, ok := cl.pending[idNum]
				cl.pendingMu.Unlock()
				if ok {
					ch <- devinRPCResult{Result: m.Result, Err: m.Error}
				}
			}
		case m.Method != "" && len(m.ID) > 0:
			// agent->client 请求：必须应答
			cl.handleAgentRequest(m)
		case m.Method == "session/update":
			cl.handleSessionUpdate(m.Params)
		}
	}
}

// handleSessionUpdate 解析 session/update 通知并回调增量文本。
func (cl *devinACPClient) handleSessionUpdate(params json.RawMessage) {
	var p struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &p); err != nil || cl.onEvent == nil {
		return
	}
	var kind string
	switch p.Update.SessionUpdate {
	case "agent_message_chunk":
		kind = "message"
	case "agent_thought_chunk":
		kind = "thought"
	default:
		return
	}
	if p.Update.Content != nil && p.Update.Content.Text != "" {
		cl.onEvent(p.SessionID, devinACPEvent{Kind: kind, Text: p.Update.Content.Text})
	}
}

// handleAgentRequest 应答 agent->client 请求。
//   - session/request_permission: autoApprove 时选第一个 allow 选项，否则取消
//   - elicitation/create: 返回 cancelled
//   - 其余（fs/terminal 等）：method_not_found（云端 agent 在自有沙箱执行，正常不会调用）
func (cl *devinACPClient) handleAgentRequest(m devinRPCMessage) {
	resp := devinRPCMessage{JSONRPC: "2.0", ID: m.ID}
	switch m.Method {
	case "session/request_permission":
		if cl.autoApprove {
			if optionID := pickAllowPermissionOption(m.Params); optionID != "" {
				resp.Result, _ = json.Marshal(map[string]any{
					"outcome": map[string]any{"outcome": "selected", "optionId": optionID},
				})
				break
			}
		}
		resp.Result, _ = json.Marshal(map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		})
	case "elicitation/create":
		resp.Result, _ = json.Marshal(map[string]any{"action": "cancelled"})
	default:
		resp.Error = &devinRPCError{Code: -32601, Message: "method_not_found"}
	}
	if frame, err := json.Marshal(resp); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), devinACPCloseTimeout)
		_ = cl.conn.Write(ctx, coderws.MessageText, frame)
		cancel()
	}
}

// pickAllowPermissionOption 从 request_permission params.options 中挑第一个
// kind 含 "allow" 的选项 ID。
func pickAllowPermissionOption(params json.RawMessage) string {
	var p struct {
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	for _, opt := range p.Options {
		if strings.Contains(opt.Kind, "allow") {
			return opt.OptionID
		}
	}
	if len(p.Options) > 0 {
		return p.Options[0].OptionID
	}
	return ""
}

// devinNewSession 创建云端 Devin 会话，返回 sessionId。
func (cl *devinACPClient) devinNewSession(ctx context.Context, cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = "/"
	}
	res, err := cl.call(ctx, "session/new", map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", err
	}
	if out.SessionID == "" {
		return "", errors.New("devin acp session/new returned empty sessionId")
	}
	return out.SessionID, nil
}

// devinSetConfig 设置会话 configOption（devin_version / org_id 等），plain string value。
func (cl *devinACPClient) devinSetConfig(ctx context.Context, sessionID, configID, value string) error {
	_, err := cl.call(ctx, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  configID,
		"value":     value,
	})
	return err
}

// devinPrompt 发起一轮对话；onEvent 经构造时注册的回调推送增量。
func (cl *devinACPClient) devinPrompt(ctx context.Context, sessionID string, prompt []map[string]any) (*devinACPTurnResult, error) {
	promptCtx, cancel := context.WithTimeout(ctx, devinACPPromptTimeout)
	defer cancel()
	res, err := cl.call(promptCtx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    prompt,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		StopReason string `json:"stopReason"`
		Usage      *struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
			TotalTokens  int `json:"totalTokens"`
		} `json:"usage"`
		Meta map[string]any `json:"_meta"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("parse session/prompt result: %w", err)
	}
	r := &devinACPTurnResult{StopReason: out.StopReason, SessionID: sessionID}
	if out.Usage != nil {
		r.InputTokens = out.Usage.InputTokens
		r.OutputTokens = out.Usage.OutputTokens
		r.TotalTokens = out.Usage.TotalTokens
	}
	if v, ok := out.Meta["cognition.ai/userMessageId"].(string); ok {
		r.UserMessageID = v
	}
	return r, nil
}

// devinCloseSession 尽力关闭远端会话（session/close + session/delete 不保证存在，忽略错误）。
func (cl *devinACPClient) devinCloseSession(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), devinACPCloseTimeout)
	defer cancel()
	_, _ = cl.call(ctx, "session/close", map[string]any{"sessionId": sessionID})
}

// devinSessionTokenCache 按账号缓存 api_key 换来的 session token（进程内存即可，
// 丢失只是多换一次，不值得写库）。
var devinSessionTokenCache sync.Map // accountID -> string(session token)

// resolveDevinSessionToken 取账号可用的 devin session token：
// 凭证直接存了 session_token / api_key 为 devin-session-token$ 前缀时原样返回；
// 否则用 api_key 调 SeatManagementService/GetSelfDevinSessionToken（Connect-JSON）换发。
func resolveDevinSessionToken(ctx context.Context, account *Account, proxyURL string) (string, error) {
	if tok := account.DevinSessionToken(); tok != "" {
		return tok, nil
	}
	apiKey := account.DevinAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("account %d missing devin session_token/api_key credential", account.ID)
	}
	if cached, ok := devinSessionTokenCache.Load(account.ID); ok {
		if tok, _ := cached.(string); tok != "" {
			return tok, nil
		}
	}

	endpoint := account.DevinAPIServerURL() + "/exa.seat_management_pb.SeatManagementService/GetSelfDevinSessionToken"
	reqBody, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"api_key":           apiKey,
			"ide_name":          "sub2api",
			"extension_name":    "sub2api",
			"extension_version": "1.0.0",
		},
	})
	httpClient := &http.Client{Timeout: devinACPDialTimeout}
	if proxy := strings.TrimSpace(proxyURL); proxy != "" {
		if parsed, err := url.Parse(proxy); err == nil {
			httpClient.Transport = &http.Transport{Proxy: http.ProxyURL(parsed)}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(reqBody)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", apiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", &UpstreamFailoverError{StatusCode: http.StatusBadGateway, RequestScopedTransient: true}
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return "", &UpstreamFailoverError{
			StatusCode:   resp.StatusCode,
			ResponseBody: respBody,
			Stage:        GatewayFailureStageAccountAuth,
			Scope:        GatewayFailureScopeAccount,
			Reason:       GatewayFailureReason("devin_session_token_mint_failed"),
		}
	}
	var out struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil || out.SessionToken == "" {
		return "", fmt.Errorf("devin GetSelfDevinSessionToken: empty sessionToken (status=%d)", resp.StatusCode)
	}
	devinSessionTokenCache.Store(account.ID, out.SessionToken)
	return out.SessionToken, nil
}
