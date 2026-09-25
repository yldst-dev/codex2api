// Copyright (C) Wei-Shaw and sub2api contributors.
// SPDX-License-Identifier: LGPL-3.0-only
//
// Derived from https://github.com/Wei-Shaw/sub2api
// commit 20a94fbb567b62208751292ed7786b24a7e7c0fe
// backend/internal/pkg/openai/oauth.go
// backend/internal/repository/openai_oauth_service.go
// backend/internal/service/openai_oauth_service.go

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	AuthorizeURL = "https://auth.openai.com/oauth/authorize"
	TokenURL     = "https://auth.openai.com/oauth/token"
	RedirectURI  = "http://localhost:1455/auth/callback"
	Scope        = "openid profile email offline_access"
	RefreshScope = "openid profile email"

	UserAgent  = "codex-tui/0.157.0 (Ubuntu 22.4.0; x86_64) xterm-256color"
	Originator = "codex-tui"
	Version    = "0.157.0"
	MinVersion = "0.144.0"

	CallbackAddr = "127.0.0.1:1455"
)

type Token struct {
	AccessToken    string
	RefreshToken   string
	IDToken        string
	ExpiresIn      int64
	ExpiresAt      time.Time
	Email          string
	AccountID      string
	UserID         string
	ChatGPTUserID  string
	PlanType       string
	OrganizationID string
}

type EndpointError struct {
	Status    int
	Code      string
	Temporary bool
}

func (e *EndpointError) Error() string {
	if e.Code != "" {
		return "oauth token endpoint: " + e.Code
	}
	return fmt.Sprintf("oauth token endpoint status %d", e.Status)
}

type Client struct {
	TokenURL string
	HTTP     *http.Client
}

func NewClient(tokenURL string) *Client {
	if strings.TrimSpace(tokenURL) == "" {
		tokenURL = TokenURL
	}
	return &Client{
		TokenURL: tokenURL,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type Flow struct {
	State       string
	Verifier    string
	RedirectURI string
	Client      *Client
}

func StartFlow(client *Client) (*Flow, string, error) {
	if client == nil {
		client = NewClient("")
	}
	state, err := randomHex(32)
	if err != nil {
		return nil, "", err
	}
	verifier, err := randomHex(64)
	if err != nil {
		return nil, "", err
	}
	flow := &Flow{
		State:       state,
		Verifier:    verifier,
		RedirectURI: RedirectURI,
		Client:      client,
	}
	return flow, flow.AuthorizationURL(), nil
}

func (f *Flow) AuthorizationURL() string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", ClientID)
	q.Set("redirect_uri", f.RedirectURI)
	q.Set("scope", Scope)
	q.Set("state", f.State)
	q.Set("code_challenge", CodeChallenge(f.Verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	return AuthorizeURL + "?" + q.Encode()
}

func (f *Flow) ExchangeCallback(ctx context.Context, raw string) (*Token, error) {
	code, state, err := ParseCallback(raw)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(f.State)) != 1 {
		return nil, fmt.Errorf("oauth state mismatch")
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("oauth code is empty")
	}
	return f.Client.ExchangeCode(ctx, code, f.Verifier, f.RedirectURI)
}

func CodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func ParseCallback(raw string) (code, state string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("callback URL is empty")
	}
	if len(raw) > 4096 {
		return "", "", fmt.Errorf("callback URL is too long")
	}
	if !strings.Contains(raw, "://") {
		if strings.HasPrefix(raw, "/") {
			raw = "http://localhost" + raw
		} else {
			raw = "http://localhost/?" + strings.TrimPrefix(raw, "?")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("callback URL is invalid")
	}
	q := u.Query()
	if q.Get("error") != "" {
		return "", "", fmt.Errorf("oauth authorization failed: %s", q.Get("error"))
	}
	code = strings.TrimSpace(q.Get("code"))
	state = strings.TrimSpace(q.Get("state"))
	if code == "" || state == "" {
		return "", "", fmt.Errorf("callback URL is missing code or state")
	}
	return code, state, nil
}

func (c *Client) ExchangeCode(ctx context.Context, code, verifier, redirectURI string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", ClientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	return c.post(ctx, form)
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", ClientID)
	form.Set("scope", RefreshScope)
	return c.post(ctx, form)
}

func (c *Client) post(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("originator", Originator)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &EndpointError{Temporary: true, Code: "network"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, &EndpointError{Status: resp.StatusCode, Temporary: true, Code: "read"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classify(resp.StatusCode, body)
	}
	var raw tokenResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &EndpointError{Status: resp.StatusCode, Temporary: true}
	}
	if strings.TrimSpace(raw.AccessToken) == "" {
		return nil, &EndpointError{Status: resp.StatusCode, Temporary: true}
	}
	tok := &Token{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		IDToken:      raw.IDToken,
		ExpiresIn:    raw.ExpiresIn,
	}
	tok.ExpiresAt = expiresAt(raw.ExpiresIn, raw.AccessToken)
	applyIdentity(tok)
	return tok, nil
}

func expiresAt(expiresIn int64, accessToken string) time.Time {
	if expiresIn > 0 {
		return time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	if claims, err := decodeClaims(accessToken); err == nil && claims.Exp > 0 {
		return time.Unix(claims.Exp, 0)
	}
	return time.Now()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func applyIdentity(tok *Token) {
	if tok.IDToken != "" {
		if info, err := ParseIDToken(tok.IDToken); err == nil {
			fillToken(tok, info)
		} else if info, err := DecodeIDToken(tok.IDToken); err == nil {
			fillToken(tok, info)
		}
	}
	if tok.AccountID == "" && tok.AccessToken != "" {
		if info, err := DecodeIDToken(tok.AccessToken); err == nil {
			fillToken(tok, info)
		}
	}
}

func fillToken(tok *Token, info *UserInfo) {
	if tok.Email == "" {
		tok.Email = info.Email
	}
	if tok.AccountID == "" {
		tok.AccountID = info.ChatGPTAccountID
	}
	if tok.UserID == "" {
		tok.UserID = info.UserID
	}
	if tok.ChatGPTUserID == "" {
		tok.ChatGPTUserID = info.ChatGPTUserID
	}
	if tok.PlanType == "" {
		tok.PlanType = info.PlanType
	}
	if tok.OrganizationID == "" {
		tok.OrganizationID = info.OrganizationID
	}
}

func classify(status int, body []byte) error {
	code := errorCode(body)
	permanent := isPermanent(code)
	switch status {
	case http.StatusUnauthorized:
		permanent = true
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		permanent = false
	}
	if status >= 500 {
		permanent = false
	}
	if code == "" && status == http.StatusBadRequest {
		permanent = false
	}
	return &EndpointError{Status: status, Code: code, Temporary: !permanent}
}

func errorCode(body []byte) string {
	var wrapped struct {
		Error            any    `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return ""
	}
	switch v := wrapped.Error.(type) {
	case string:
		return cleanCode(v)
	case map[string]any:
		if code, ok := v["code"].(string); ok {
			if cleaned := cleanCode(code); cleaned != "" {
				return cleaned
			}
		}
	}
	return cleanCode(wrapped.ErrorDescription)
}

func cleanCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return ""
		}
	}
	return value
}

func isPermanent(code string) bool {
	if code == "" {
		return false
	}
	needles := []string{
		"invalid_grant",
		"invalid_refresh_token",
		"token_expired",
		"app_session_terminated",
		"refresh_token_reused",
		"refresh_token_invalidated",
		"invalid_client",
		"unauthorized_client",
		"access_denied",
	}
	for _, needle := range needles {
		if strings.Contains(code, needle) {
			return true
		}
	}
	return false
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

type IDTokenClaims struct {
	Sub        string      `json:"sub"`
	Email      string      `json:"email"`
	Exp        int64       `json:"exp"`
	OpenAIAuth *AuthClaims `json:"https://api.openai.com/auth,omitempty"`
	Aud        Audience    `json:"aud"`
}

type AuthClaims struct {
	ChatGPTAccountID string         `json:"chatgpt_account_id"`
	ChatGPTUserID    string         `json:"chatgpt_user_id"`
	ChatGPTPlanType  string         `json:"chatgpt_plan_type"`
	UserID           string         `json:"user_id"`
	POID             string         `json:"poid"`
	Organizations    []Organization `json:"organizations"`
}

type Organization struct {
	ID        string `json:"id"`
	IsDefault bool   `json:"is_default"`
}

type Audience []string

func (a *Audience) UnmarshalJSON(b []byte) error {
	if string(b) == "null" || len(b) == 0 {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*a = []string{s}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

type UserInfo struct {
	Email            string
	ChatGPTAccountID string
	ChatGPTUserID    string
	PlanType         string
	UserID           string
	OrganizationID   string
}

func ParseIDToken(raw string) (*UserInfo, error) {
	claims, err := decodeClaims(raw)
	if err != nil {
		return nil, err
	}
	const skew = int64(120)
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp+skew {
		return nil, fmt.Errorf("id token expired")
	}
	return claims.userInfo(), nil
}

func DecodeIDToken(raw string) (*UserInfo, error) {
	claims, err := decodeClaims(raw)
	if err != nil {
		return nil, err
	}
	return claims.userInfo(), nil
}

func decodeClaims(raw string) (*IDTokenClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid jwt")
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid jwt payload")
		}
	}
	var claims IDTokenClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("invalid jwt claims")
	}
	return &claims, nil
}

func (c *IDTokenClaims) userInfo() *UserInfo {
	info := &UserInfo{Email: c.Email}
	if c.OpenAIAuth == nil {
		return info
	}
	info.ChatGPTAccountID = c.OpenAIAuth.ChatGPTAccountID
	info.ChatGPTUserID = c.OpenAIAuth.ChatGPTUserID
	info.PlanType = c.OpenAIAuth.ChatGPTPlanType
	info.UserID = c.OpenAIAuth.UserID
	if info.UserID == "" {
		info.UserID = c.OpenAIAuth.ChatGPTUserID
	}
	for _, org := range c.OpenAIAuth.Organizations {
		if org.IsDefault && org.ID != "" {
			info.OrganizationID = org.ID
			break
		}
	}
	if info.OrganizationID == "" && len(c.OpenAIAuth.Organizations) > 0 {
		info.OrganizationID = c.OpenAIAuth.Organizations[0].ID
	}
	if info.OrganizationID == "" {
		info.OrganizationID = c.OpenAIAuth.POID
	}
	return info
}

func ValidClientVersion(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 64 {
		return false
	}
	core := version
	if i := strings.IndexByte(version, '-'); i >= 0 {
		core = version[:i]
		suffix := version[i+1:]
		if suffix == "" || strings.ContainsAny(suffix, " /") {
			return false
		}
	}
	parts := strings.Split(core, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func CompareVersions(a, b string) int {
	as := versionParts(a)
	bs := versionParts(b)
	for i := 0; i < 3; i++ {
		if as[i] > bs[i] {
			return 1
		}
		if as[i] < bs[i] {
			return -1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	parts := strings.Split(v, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n := 0
		for _, c := range parts[i] {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out[i] = n
	}
	return out
}

func HeaderVersion(clientVersion string) string {
	if ValidClientVersion(clientVersion) && CompareVersions(clientVersion, MinVersion) >= 0 {
		return strings.TrimSpace(clientVersion)
	}
	return Version
}
