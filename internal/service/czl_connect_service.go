package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/logger"
)

// CZL Connect OAuth2 endpoints（相对 BaseURL）
const (
	czlConnectAuthorizePath = "/oauth2/authorize"
	czlConnectTokenPath     = "/api/oauth2/token"
	czlConnectUserInfoPath  = "/api/oauth2/userinfo"
	czlConnectStateRedisKey = "oauth2:czl_connect:state:%s"
)

// CZLConnectService CZL Connect OAuth2 客户端服务
// 负责授权 URL 生成、授权码换取令牌、令牌刷新与用户信息获取。
type CZLConnectService struct {
	cfg    config.CZLConnectConfig
	client *http.Client
}

// NewCZLConnectService 创建 CZL Connect 服务
func NewCZLConnectService(cfg config.CZLConnectConfig) *CZLConnectService {
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 10
	}
	return &CZLConnectService{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
}

// Enabled 判断 CZL Connect 登录是否启用且配置完整
func (s *CZLConnectService) Enabled() bool {
	if s == nil {
		return false
	}
	if !s.cfg.Enabled {
		return false
	}
	return strings.TrimSpace(s.cfg.ClientID) != "" &&
		strings.TrimSpace(s.cfg.ClientSecret) != "" &&
		strings.TrimSpace(s.cfg.RedirectURI) != ""
}

// CZLConnectAuthorizeRequest 构建授权 URL 的输入
type CZLConnectAuthorizeRequest struct {
	// ReturnTo 授权完成后应用侧跳转地址，回调处理完成后由调用方使用
	ReturnTo string
	// Scopes 额外覆盖，空则使用配置
	Scopes []string
	// UpstreamProviders 限定的上游提供方，如 "github,google"
	UpstreamProviders string
}

// CZLConnectAuthorizeResult 构建授权 URL 的结果
type CZLConnectAuthorizeResult struct {
	AuthorizeURL string
	State        string
}

// BuildAuthorizeURL 生成跳转到 CZL Connect 的授权链接
// 同时在 Redis 中存储 state 与 code_verifier（PKCE），供回调校验。
func (s *CZLConnectService) BuildAuthorizeURL(ctx context.Context, req CZLConnectAuthorizeRequest) (*CZLConnectAuthorizeResult, error) {
	if !s.Enabled() {
		return nil, ErrCZLConnectDisabled
	}

	state, err := randomURLSafe(24)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", s.cfg.ClientID)
	query.Set("redirect_uri", s.cfg.RedirectURI)
	query.Set("state", state)

	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = s.cfg.Scopes
	}
	if len(scopes) > 0 {
		query.Set("scope", strings.Join(scopes, " "))
	}
	if trimmed := strings.TrimSpace(req.UpstreamProviders); trimmed != "" {
		query.Set("upstream_providers", trimmed)
	}

	var codeVerifier string
	if s.cfg.UsePKCE {
		codeVerifier, err = randomURLSafe(48)
		if err != nil {
			return nil, err
		}
		challenge := pkceS256Challenge(codeVerifier)
		query.Set("code_challenge", challenge)
		query.Set("code_challenge_method", "S256")
	}

	if err := s.saveState(ctx, state, codeVerifier, strings.TrimSpace(req.ReturnTo)); err != nil {
		return nil, err
	}

	return &CZLConnectAuthorizeResult{
		AuthorizeURL: s.buildURL(czlConnectAuthorizePath) + "?" + query.Encode(),
		State:        state,
	}, nil
}

// CZLConnectTokenResult token 端点响应
type CZLConnectTokenResult struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// CZLConnectUserInfo userinfo 端点响应
type CZLConnectUserInfo struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	Email     string `json:"email"`
	Avatar    string `json:"avatar"`
	Groups    string `json:"groups"`
	Upstreams []any  `json:"upstreams"`
}

// CZLConnectCallbackInput 授权回调输入
type CZLConnectCallbackInput struct {
	Code  string
	State string
}

// CZLConnectCallbackResult 授权回调结果
type CZLConnectCallbackResult struct {
	Token    CZLConnectTokenResult
	UserInfo CZLConnectUserInfo
	ReturnTo string
}

// HandleCallback 处理授权回调：校验 state、交换 token、拉取 userinfo
func (s *CZLConnectService) HandleCallback(ctx context.Context, input CZLConnectCallbackInput) (*CZLConnectCallbackResult, error) {
	if !s.Enabled() {
		return nil, ErrCZLConnectDisabled
	}
	state := strings.TrimSpace(input.State)
	code := strings.TrimSpace(input.Code)
	if state == "" || code == "" {
		return nil, ErrCZLConnectStateInvalid
	}

	stored, err := s.loadState(ctx, state)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, ErrCZLConnectStateInvalid
	}
	// 一次性消费，避免重放
	_ = s.deleteState(ctx, state)

	token, err := s.exchangeCode(ctx, code, stored.CodeVerifier)
	if err != nil {
		return nil, err
	}

	userInfo, err := s.fetchUserInfo(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}

	return &CZLConnectCallbackResult{
		Token:    *token,
		UserInfo: *userInfo,
		ReturnTo: stored.ReturnTo,
	}, nil
}

// RefreshToken 使用 refresh_token 刷新访问令牌
func (s *CZLConnectService) RefreshToken(ctx context.Context, refreshToken string) (*CZLConnectTokenResult, error) {
	if !s.Enabled() {
		return nil, ErrCZLConnectDisabled
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, ErrCZLConnectTokenInvalid
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", s.cfg.ClientID)
	form.Set("client_secret", s.cfg.ClientSecret)
	return s.postToken(ctx, form)
}

// FetchUserInfo 使用访问令牌获取用户信息
func (s *CZLConnectService) FetchUserInfo(ctx context.Context, accessToken string) (*CZLConnectUserInfo, error) {
	if !s.Enabled() {
		return nil, ErrCZLConnectDisabled
	}
	return s.fetchUserInfo(ctx, accessToken)
}

// exchangeCode 用授权码换取令牌（client_secret_post + 可选 PKCE）
func (s *CZLConnectService) exchangeCode(ctx context.Context, code, codeVerifier string) (*CZLConnectTokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", s.cfg.RedirectURI)
	form.Set("client_id", s.cfg.ClientID)
	form.Set("client_secret", s.cfg.ClientSecret)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	return s.postToken(ctx, form)
}

// postToken 调用 token 端点并解析响应；非 2xx 返回 ErrCZLConnectTokenFailed
func (s *CZLConnectService) postToken(ctx context.Context, form url.Values) (*CZLConnectTokenResult, error) {
	endpoint := s.buildURL(czlConnectTokenPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		logger.Warnw("czl_connect_token_http_failed", "error", err)
		return nil, ErrCZLConnectTokenFailed
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warnw("czl_connect_token_bad_status", "status", resp.StatusCode, "body", truncateForLog(body))
		return nil, ErrCZLConnectTokenFailed
	}
	var result CZLConnectTokenResult
	if err := json.Unmarshal(body, &result); err != nil {
		logger.Warnw("czl_connect_token_decode_failed", "error", err, "body", truncateForLog(body))
		return nil, ErrCZLConnectTokenFailed
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return nil, ErrCZLConnectTokenFailed
	}
	return &result, nil
}

// fetchUserInfo 拉取 userinfo；非 2xx 返回 ErrCZLConnectUserInfoFailed
func (s *CZLConnectService) fetchUserInfo(ctx context.Context, accessToken string) (*CZLConnectUserInfo, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, ErrCZLConnectUserInfoFailed
	}
	endpoint := s.buildURL(czlConnectUserInfoPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		logger.Warnw("czl_connect_userinfo_http_failed", "error", err)
		return nil, ErrCZLConnectUserInfoFailed
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warnw("czl_connect_userinfo_bad_status", "status", resp.StatusCode, "body", truncateForLog(body))
		return nil, ErrCZLConnectUserInfoFailed
	}
	var info CZLConnectUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		logger.Warnw("czl_connect_userinfo_decode_failed", "error", err, "body", truncateForLog(body))
		return nil, ErrCZLConnectUserInfoFailed
	}
	if info.ID == 0 {
		return nil, ErrCZLConnectUserInfoFailed
	}
	return &info, nil
}

// buildURL 拼接 CZL Connect 服务端点，去除 base_url 末尾 /
func (s *CZLConnectService) buildURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(s.cfg.BaseURL), "/")
	if base == "" {
		base = "https://connect.czl.net"
	}
	return base + path
}

// czlConnectStateRecord 保存在 Redis 中的授权上下文
type czlConnectStateRecord struct {
	CodeVerifier string `json:"code_verifier,omitempty"`
	ReturnTo     string `json:"return_to,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

func (s *CZLConnectService) stateTTL() time.Duration {
	ttl := s.cfg.StateTTL
	if ttl <= 0 {
		ttl = 600
	}
	return time.Duration(ttl) * time.Second
}

func (s *CZLConnectService) saveState(ctx context.Context, state, codeVerifier, returnTo string) error {
	record := czlConnectStateRecord{
		CodeVerifier: codeVerifier,
		ReturnTo:     returnTo,
		CreatedAt:    time.Now().Unix(),
	}
	return cache.SetJSON(ctx, fmt.Sprintf(czlConnectStateRedisKey, state), record, s.stateTTL())
}

func (s *CZLConnectService) loadState(ctx context.Context, state string) (*czlConnectStateRecord, error) {
	var record czlConnectStateRecord
	found, err := cache.GetJSON(ctx, fmt.Sprintf(czlConnectStateRedisKey, state), &record)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &record, nil
}

func (s *CZLConnectService) deleteState(ctx context.Context, state string) error {
	return cache.Del(ctx, fmt.Sprintf(czlConnectStateRedisKey, state))
}

// randomURLSafe 生成 URL 安全的随机字符串
func randomURLSafe(byteLen int) (string, error) {
	if byteLen <= 0 {
		byteLen = 32
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// pkceS256Challenge 按 RFC 7636 生成 S256 code_challenge
func pkceS256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// truncateForLog 截断响应体，避免日志暴涨
func truncateForLog(body []byte) string {
	const max = 512
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "...(truncated)"
}
