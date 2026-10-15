package service

// WorkBuddy 协议移植自 magpie-community/plugins/packages/workbuddy 0.1.22。
// Copyright (c) 2026 magpie-community contributors，MIT，见 docs/WORKBUDDY_LICENSE.txt。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	workBuddyVersion   = "5.5.6"
	workBuddyLoginTTL  = 5 * time.Minute
	workBuddyBodyLimit = 8 << 20
	// RFC3339 可表示的最大毫秒时间戳（9999 年末）。
	workBuddyMaxExpiryMillis = 253402300799999
)

type WorkBuddyService struct {
	http       HTTPUpstream
	proxyRepo  ProxyRepository
	accounts   AccountRepository
	refreshAPI *OAuthRefreshAPI
	loginMu    sync.Mutex
	logins     map[string]*workBuddyLogin
}

type workBuddyLogin struct {
	mu                     *contextMutex
	owner                  int64
	platform, state, proxy string
	expires                time.Time
	credentials            map[string]any
	ready                  bool
}

type WorkBuddyLoginResult struct {
	SessionID   string         `json:"session_id,omitempty"`
	AuthURL     string         `json:"auth_url,omitempty"`
	ExpiresAt   time.Time      `json:"expires_at"`
	Status      string         `json:"status"`
	Credentials map[string]any `json:"credentials,omitempty"`
}

func NewWorkBuddyService(httpUpstream HTTPUpstream, proxyRepo ProxyRepository, accounts AccountRepository, refreshAPI *OAuthRefreshAPI) *WorkBuddyService {
	return &WorkBuddyService{http: httpUpstream, proxyRepo: proxyRepo, accounts: accounts, refreshAPI: refreshAPI, logins: make(map[string]*workBuddyLogin)}
}

func workBuddyEndpoint(platform string) (string, error) {
	switch platform {
	case PlatformWorkBuddy:
		return "https://copilot.tencent.com", nil
	case PlatformWorkBuddyAI:
		return "https://www.workbuddy.ai", nil
	default:
		return "", errors.New("unsupported WorkBuddy platform")
	}
}

func workBuddyID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(b[:])
}

func workBuddyString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}

func workBuddyNumber(v any) (float64, error) {
	if v == nil || v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(workBuddyString(v), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, errors.New("invalid WorkBuddy credit amount")
	}
	return f, nil
}

type workBuddyAPIError struct{ Code, Status int }

func (e *workBuddyAPIError) Error() string {
	switch e.Code {
	case 14018:
		return "WorkBuddy credits exhausted; an active credit package is required"
	case 11140:
		return "WorkBuddy account is restricted; submit feedback or appeal in WorkBuddy"
	case 11217, 12151:
		return "WorkBuddy sign-in is pending"
	default:
		return fmt.Sprintf("WorkBuddy request failed (HTTP %d, code %d)", e.Status, e.Code)
	}
}

func workBuddyHeaders(platform string, credentials map[string]any) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("User-Agent", "WorkBuddy/"+workBuddyVersion)
	h.Set("X-Product", "SaaS")
	h.Set("X-IDE-Type", "WorkBuddy")
	h.Set("X-Requested-With", "XMLHttpRequest")
	endpoint, _ := workBuddyEndpoint(platform)
	u, _ := url.Parse(endpoint)
	domain := workBuddyString(credentials["domain"])
	if domain == "" && u != nil {
		domain = u.Hostname()
	}
	h.Set("X-Domain", domain)
	if access := workBuddyString(credentials["access_token"]); access != "" {
		h.Set("Authorization", "Bearer "+access)
	}
	if uid := workBuddyString(credentials["uid"]); uid != "" {
		h.Set("X-User-Id", uid)
	}
	if id := workBuddyString(credentials["enterprise_id"]); id != "" {
		h.Set("X-Enterprise-Id", id)
		h.Set("X-Tenant-Id", id)
	}
	return h
}

func (s *WorkBuddyService) call(ctx context.Context, platform, proxy, method, path string, credentials map[string]any, headers http.Header, body any) (map[string]any, error) {
	endpoint, err := workBuddyEndpoint(platform)
	if err != nil {
		return nil, err
	}
	if s == nil || s.http == nil {
		return nil, errors.New("WorkBuddy transport is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header = workBuddyHeaders(platform, credentials)
	for key, values := range headers {
		if len(values) == 0 {
			req.Header.Del(key)
			continue
		}
		req.Header[key] = values
	}
	resp, err := s.http.Do(req, proxy, 0, 1)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("WorkBuddy control API connection failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, workBuddyBodyLimit+1))
	if err != nil {
		return nil, errors.New("failed to read WorkBuddy response")
	}
	if len(data) > workBuddyBodyLimit {
		return nil, errors.New("WorkBuddy response is too large")
	}
	var envelope struct {
		Code json.Number    `json:"code"`
		Data map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	err = decoder.Decode(&envelope)
	code, _ := strconv.Atoi(envelope.Code.String())
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || code != 0 {
		return nil, &workBuddyAPIError{Code: code, Status: resp.StatusCode}
	}
	if err != nil || envelope.Data == nil {
		return nil, errors.New("invalid WorkBuddy response envelope")
	}
	return envelope.Data, nil
}

func (s *WorkBuddyService) proxyURL(ctx context.Context, id *int64) (string, error) {
	if id == nil {
		return "", nil
	}
	p, err := s.proxyRepo.GetByID(ctx, *id)
	if err != nil || p == nil {
		return "", errors.New("WorkBuddy proxy is unavailable")
	}
	return p.URL(), nil
}

func (s *WorkBuddyService) StartLogin(ctx context.Context, owner int64, platform string, proxyID *int64) (*WorkBuddyLoginResult, error) {
	proxy, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	authPlatform := platform
	if platform == PlatformWorkBuddyAI {
		authPlatform = "workbuddy-ai"
	}
	h := http.Header{}
	for _, key := range []string{"X-No-Authorization", "X-No-User-Id", "X-No-Enterprise-Id", "X-No-Department-Info"} {
		h.Set(key, "true")
	}
	data, err := s.call(ctx, platform, proxy, http.MethodPost, "/v2/plugin/auth/state?platform="+url.QueryEscape(authPlatform), nil, h, map[string]any{})
	if err != nil {
		return nil, err
	}
	state := workBuddyString(data["state"])
	u, err := url.Parse(workBuddyString(data["authUrl"]))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || state == "" {
		return nil, errors.New("invalid WorkBuddy login URL or state")
	}
	q := u.Query()
	q.Set("version", "2.0.0")
	q.Set("loginSessionId", workBuddyID())
	u.RawQuery = q.Encode()
	id := workBuddyID()
	expires := time.Now().Add(workBuddyLoginTTL)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	for key, item := range s.logins {
		if time.Now().After(item.expires) {
			delete(s.logins, key)
		}
	}
	if len(s.logins) >= 1000 {
		return nil, errors.New("too many pending WorkBuddy logins; retry later")
	}
	s.logins[id] = &workBuddyLogin{mu: newContextMutex(), owner: owner, platform: platform, state: state, proxy: proxy, expires: expires}
	return &WorkBuddyLoginResult{SessionID: id, AuthURL: u.String(), ExpiresAt: expires, Status: "pending"}, nil
}

func workBuddyPending(err error, code int) bool {
	var apiErr *workBuddyAPIError
	return errors.As(err, &apiErr) && (apiErr.Code == code || apiErr.Status == 408 || apiErr.Status == 429)
}

func (s *WorkBuddyService) PollLogin(ctx context.Context, owner int64, id string) (*WorkBuddyLoginResult, error) {
	s.loginMu.Lock()
	session := s.logins[id]
	s.loginMu.Unlock()
	if session == nil || session.owner != owner || time.Now().After(session.expires) {
		return nil, errors.New("WorkBuddy login expired or not found; start again")
	}
	if err := session.mu.Lock(ctx); err != nil {
		return nil, err
	}
	defer session.mu.Unlock()
	out := &WorkBuddyLoginResult{Status: "pending", ExpiresAt: session.expires}
	if session.ready {
		out.Status = "ready"
		out.Credentials = shallowCopyMap(session.credentials)
		return out, nil
	}
	if session.credentials == nil {
		data, err := s.call(ctx, session.platform, session.proxy, http.MethodGet, "/v2/plugin/auth/token?state="+url.QueryEscape(session.state), nil, nil, nil)
		if workBuddyPending(err, 11217) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		session.credentials, err = workBuddyTokens(data, nil)
		if err != nil {
			return nil, err
		}
	}
	h := http.Header{"X-No-User-Id": {"true"}, "X-No-Enterprise-Id": {"true"}}
	who, err := s.call(ctx, session.platform, session.proxy, http.MethodGet, "/v2/plugin/login/account?state="+url.QueryEscape(session.state), session.credentials, h, nil)
	if workBuddyPending(err, 12151) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	uid := workBuddyString(who["uid"])
	if uid == "" {
		return nil, errors.New("WorkBuddy login returned no user ID")
	}
	session.credentials["uid"] = uid
	session.credentials["nickname"] = workBuddyString(who["nickname"])
	kind := strings.ToLower(workBuddyString(who["type"]))
	session.credentials["account_type"] = kind
	enterprise := workBuddyString(who["enterpriseId"])
	if enterprise == "" && kind != "" && kind != "personal" {
		list, err := s.call(ctx, session.platform, session.proxy, http.MethodGet, "/v2/plugin/accounts", session.credentials, nil, nil)
		if err != nil {
			return nil, err
		}
		enterprise = workBuddyEnterprise(list["accounts"], uid, kind)
		if enterprise == "" {
			return nil, errors.New("WorkBuddy enterprise is ambiguous; import credentials with an explicit enterpriseId")
		}
	}
	if enterprise != "" {
		session.credentials["enterprise_id"] = enterprise
	}
	if err := ValidateWorkBuddyCredentials(session.credentials); err != nil {
		return nil, err
	}
	session.ready = true
	out.Status = "ready"
	out.Credentials = shallowCopyMap(session.credentials)
	return out, nil
}

func workBuddyEnterprise(raw any, uid, kind string) string {
	list, _ := raw.([]any)
	var all, same, last []string
	for _, v := range list {
		x, _ := v.(map[string]any)
		id := workBuddyString(x["enterpriseId"])
		if id == "" || workBuddyString(x["uid"]) != uid {
			continue
		}
		all = append(all, id)
		if strings.EqualFold(workBuddyString(x["type"]), kind) {
			same = append(same, id)
		}
		if x["lastLogin"] == true {
			last = append(last, id)
		}
	}
	for _, candidates := range [][]string{same, last, all} {
		if len(candidates) == 1 {
			return candidates[0]
		}
	}
	return ""
}

func workBuddyTokens(raw, previous map[string]any) (map[string]any, error) {
	out := shallowCopyMap(previous)
	if out == nil {
		out = make(map[string]any)
	}
	for remote, local := range map[string]string{"accessToken": "access_token", "refreshToken": "refresh_token", "domain": "domain", "tokenType": "token_type"} {
		if v, exists := raw[remote]; exists {
			if _, ok := v.(map[string]any); ok {
				return nil, errors.New("WorkBuddy desktop tokens are encrypted; use browser sign-in")
			}
			if value := workBuddyString(v); value != "" {
				out[local] = value
			}
		}
	}
	for _, spec := range [][3]string{{"expiresAt", "expiresIn", "expires_at"}, {"refreshExpiresAt", "refreshExpiresIn", "refresh_expires_at"}} {
		ms, err := workBuddyNumber(raw[spec[0]])
		if err != nil {
			return nil, errors.New("invalid WorkBuddy token expiry")
		}
		if ms > 0 {
			if ms > workBuddyMaxExpiryMillis {
				return nil, errors.New("invalid WorkBuddy token expiry")
			}
			out[spec[2]] = time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339)
			continue
		}
		seconds, err := workBuddyNumber(raw[spec[1]])
		if err != nil {
			return nil, errors.New("invalid WorkBuddy token lifetime")
		}
		if seconds > 0 && seconds < 10*365*24*3600 {
			out[spec[2]] = time.Now().Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
		}
	}
	if workBuddyString(out["access_token"]) == "" {
		return nil, errors.New("WorkBuddy access token is missing")
	}
	return out, nil
}

func ValidateWorkBuddyCredentials(credentials map[string]any) error {
	for _, key := range []string{"access_token", "uid"} {
		if workBuddyString(credentials[key]) == "" {
			return fmt.Errorf("WorkBuddy %s is required", key)
		}
	}
	for _, key := range []string{"access_token", "refresh_token", "uid", "domain", "enterprise_id"} {
		v := workBuddyString(credentials[key])
		if strings.ContainsAny(v, "\r\n") || len(v) > 65536 {
			return fmt.Errorf("invalid WorkBuddy %s", key)
		}
	}
	kind := strings.ToLower(workBuddyString(credentials["account_type"]))
	if kind != "" && kind != "personal" && workBuddyString(credentials["enterprise_id"]) == "" {
		return errors.New("WorkBuddy team credentials require enterprise_id")
	}
	if v := workBuddyString(credentials["expires_at"]); v != "" {
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			return errors.New("WorkBuddy expires_at must use RFC3339")
		}
	}
	return nil
}

func ImportWorkBuddyCredentials(raw json.RawMessage) (map[string]any, error) {
	var file struct {
		Auth    map[string]any `json:"auth"`
		Account map[string]any `json:"account"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if len(raw) > 1<<20 || d.Decode(&file) != nil || file.Auth == nil || file.Account == nil {
		return nil, errors.New("expected WorkBuddy desktop JSON with auth and account")
	}
	credentials, err := workBuddyTokens(file.Auth, nil)
	if err != nil {
		return nil, err
	}
	for remote, local := range map[string]string{"uid": "uid", "nickname": "nickname", "enterpriseId": "enterprise_id", "type": "account_type"} {
		credentials[local] = workBuddyString(file.Account[remote])
	}
	return credentials, ValidateWorkBuddyCredentials(credentials)
}

func (s *WorkBuddyService) CanRefresh(a *Account) bool {
	return a.IsWorkBuddy() && a.Type == AccountTypeOAuth && a.GetCredential("refresh_token") != ""
}
func (s *WorkBuddyService) CacheKey(a *Account) string {
	return fmt.Sprintf("workbuddy:%s:%d", a.Platform, a.ID)
}
func (s *WorkBuddyService) NeedsRefresh(a *Account, window time.Duration) bool {
	if !s.CanRefresh(a) {
		return false
	}
	expires := a.GetCredentialAsTime("expires_at")
	return expires == nil || time.Until(*expires) <= window
}

func (s *WorkBuddyService) Refresh(ctx context.Context, a *Account) (map[string]any, error) {
	if !s.CanRefresh(a) {
		return nil, errors.New("WorkBuddy refresh token is missing")
	}
	if expires := a.GetCredentialAsTime("refresh_expires_at"); expires != nil && !expires.After(time.Now()) {
		return nil, errors.New("WorkBuddy refresh token expired; sign in again")
	}
	proxy, err := s.proxyURL(ctx, a.ProxyID)
	if err != nil {
		return nil, err
	}
	h := http.Header{"X-Refresh-Token": {a.GetCredential("refresh_token")}, "X-Auth-Refresh-Source": {"plugin"}}
	// 刷新只使用 refresh token，避免过期 Bearer 被认证中间件提前拒绝。
	h["Authorization"] = nil
	h["X-User-Id"] = nil
	data, err := s.call(ctx, a.Platform, proxy, http.MethodPost, "/v2/plugin/auth/token/refresh", a.Credentials, h, map[string]any{})
	if err != nil {
		return nil, err
	}
	if workBuddyString(data["accessToken"]) == "" {
		return nil, errors.New("WorkBuddy refresh returned no access token")
	}
	return workBuddyTokens(data, a.Credentials)
}

func (s *WorkBuddyService) FreshAccount(ctx context.Context, a *Account) (*Account, error) {
	if a == nil || !a.IsWorkBuddy() || a.Type != AccountTypeOAuth {
		return nil, errors.New("WorkBuddy requires an OAuth account")
	}
	if err := ValidateWorkBuddyCredentials(a.Credentials); err != nil {
		return nil, err
	}
	if s.NeedsRefresh(a, 5*time.Minute) && a.ID > 0 {
		result, err := s.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(ctx), a, s, 5*time.Minute)
		if result != nil && result.Account != nil {
			a = result.Account
		}
		if err != nil {
			if errors.Is(err, errOAuthRefreshAccountRereadFailed) || errors.Is(err, errOAuthRefreshAccountStateChanged) || errors.Is(err, errOAuthRefreshCredentialPersist) {
				return nil, err
			}
			expires := a.GetCredentialAsTime("expires_at")
			if expires == nil || !expires.After(time.Now()) {
				return nil, err
			}
		}
	}
	if expires := a.GetCredentialAsTime("expires_at"); expires != nil && !expires.After(time.Now()) {
		return nil, errors.New("WorkBuddy access token expired; sign in again or wait for refresh")
	}
	return a, nil
}

func (s *AccountTestService) WorkBuddy() *WorkBuddyService {
	if s == nil || s.openaiGatewayService == nil {
		return nil
	}
	return s.openaiGatewayService.workBuddyService
}

func (s *WorkBuddyService) RefreshAccount(ctx context.Context, account *Account) (*Account, error) {
	result, err := s.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(ctx), account, s, 100*365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if result == nil || result.LockHeld || result.Account == nil {
		return nil, errors.New("WorkBuddy token refresh is busy; retry later")
	}
	return result.Account, nil
}
