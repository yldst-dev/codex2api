package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"codex-gateway/internal/apikey"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/store"
	"codex-gateway/internal/token"
)

const (
	defaultResponsesURL = "https://chatgpt.com/backend-api/codex/responses"
	defaultModelsURL    = "https://chatgpt.com/backend-api/codex/models"
	maxBody             = 32 << 20
	maxUpstreamBody     = 32 << 20
)

type Gateway struct {
	Tokens       *token.Manager
	Keys         *apikey.Service
	ResponsesURL string
	ModelsURL    string
	Client       *http.Client
	Log          *slog.Logger

	clientOnce sync.Once
	clientOwn  *http.Client
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", g.health)
	mux.HandleFunc("GET /v1/models", g.models)
	mux.HandleFunc("GET /models", g.models)
	mux.HandleFunc("GET /backend-api/codex/models", g.models)
	mux.HandleFunc("POST /v1/responses", g.responses)
	mux.HandleFunc("POST /responses", g.responses)
	mux.HandleFunc("POST /backend-api/codex/responses", g.responses)
	mux.HandleFunc("POST /v1/responses/compact", g.responses)
	mux.HandleFunc("POST /responses/compact", g.responses)
	mux.HandleFunc("POST /backend-api/codex/responses/compact", g.responses)
	return mux
}

func (g *Gateway) logger() *slog.Logger {
	if g.Log != nil {
		return g.Log
	}
	return slog.Default()
}

func (g *Gateway) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	g.clientOnce.Do(func() {
		g.clientOwn = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout: 15 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 10 * time.Minute,
				IdleConnTimeout:       90 * time.Second,
				ExpectContinueTimeout: time.Second,
				MaxIdleConns:          10,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	})
	return g.clientOwn
}

func (g *Gateway) health(w http.ResponseWriter, r *http.Request) {
	oauthReady := false
	if g.Tokens != nil {
		acc, err := g.Tokens.Account(r.Context())
		oauthReady = err == nil && acc != nil && acc.Status == store.OAuthActive && acc.RefreshToken != ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "oauth": oauthReady})
}

func (g *Gateway) models(w http.ResponseWriter, r *http.Request) {
	access, acc, ok := g.authorize(w, r)
	if !ok {
		return
	}
	endpoint, err := g.modelsEndpoint(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "upstream URL is invalid")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "upstream request failed")
		return
	}
	req.Host = "chatgpt.com"
	g.authHeaders(req, access, acc)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("version", oauth.HeaderVersion(r.URL.Query().Get("client_version")))
	if match := r.Header.Get("If-None-Match"); match != "" && !strings.ContainsAny(match, "\r\n") {
		req.Header.Set("If-None-Match", match)
	}
	g.forward(w, r, req, access, false, strings.HasSuffix(r.URL.Path, "/v1/models"))
}

func (g *Gateway) responses(w http.ResponseWriter, r *http.Request) {
	if rejectedQueryCredential(w, r) {
		return
	}
	access, acc, ok := g.authorize(w, r)
	if !ok {
		return
	}
	body, err := readLimited(w, r.Body, maxBody)
	if err != nil {
		return
	}
	prepared, err := prepareBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	compact := strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/compact")
	endpoint := strings.TrimRight(g.responsesBase(), "/")
	if compact {
		endpoint += "/compact"
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(prepared))
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "upstream request failed")
		return
	}
	req.Host = "chatgpt.com"
	req.ContentLength = int64(len(prepared))
	g.authHeaders(req, access, acc)
	req.Header.Set("version", oauth.ClientVersion())
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	if compact {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", "text/event-stream")
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" || strings.ContainsAny(contentType, "\r\n") {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	copyClientHeaders(r.Header, req.Header)
	if req.Header.Get("x-codex-beta-features") == "" {
		req.Header.Set("x-codex-beta-features", "remote_compaction_v2")
	}
	g.forward(w, r, req, access, !compact, false)
}

func (g *Gateway) authorize(w http.ResponseWriter, r *http.Request) (string, *store.OAuthAccount, bool) {
	if rejectedQueryCredential(w, r) {
		return "", nil, false
	}
	presented, err := bearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return "", nil, false
	}
	if _, err := g.Keys.Authenticate(r.Context(), presented); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return "", nil, false
	}
	access, acc, err := g.Tokens.AccessToken(r.Context())
	if err != nil {
		if errors.Is(err, token.ErrReauth) {
			writeError(w, http.StatusServiceUnavailable, "api_error", token.ReauthMessage)
			return "", nil, false
		}
		var temporary *token.TemporaryError
		if errors.As(err, &temporary) {
			writeError(w, http.StatusServiceUnavailable, "api_error", "Codex token refresh failed temporarily. Retry shortly.")
			return "", nil, false
		}
		g.logger().Warn("codex token lookup failed")
		writeError(w, http.StatusServiceUnavailable, "api_error", "Codex token is unavailable")
		return "", nil, false
	}
	if strings.TrimSpace(acc.AccountID) == "" {
		writeError(w, http.StatusServiceUnavailable, "api_error", "OAuth account is missing chatgpt_account_id. Run codex-gateway auth login")
		return "", nil, false
	}
	return access, acc, true
}

func (g *Gateway) authHeaders(req *http.Request, access string, acc *store.OAuthAccount) {
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("chatgpt-account-id", acc.AccountID)
	req.Header.Set("User-Agent", oauth.ClientUserAgent())
	req.Header.Set("originator", oauth.Originator)
}

func (g *Gateway) forward(w http.ResponseWriter, r *http.Request, upstream *http.Request, access string, allowStream bool, openaiList bool) {
	resp, err := g.client().Do(upstream)
	if err != nil {
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}
		g.logger().Warn("upstream request failed")
		writeError(w, http.StatusBadGateway, "api_error", "upstream request failed")
		return
	}
	defer resp.Body.Close()
	copyResponseHeaders(w.Header(), resp.Header)
	if resp.StatusCode == http.StatusNotModified {
		w.WriteHeader(resp.StatusCode)
		return
	}
	stream := allowStream && resp.StatusCode >= 200 && resp.StatusCode < 300 && streamable(resp.Header.Get("Content-Type"))
	if stream {
		if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		if w.Header().Get("Cache-Control") == "" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(resp.StatusCode)
		if err := streamCopy(w, resp.Body); err != nil && r.Context().Err() == nil {
			g.logger().Warn("upstream stream copy failed")
		}
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody+1))
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "upstream response failed")
		return
	}
	if len(body) > maxUpstreamBody {
		g.logger().Warn("upstream response exceeded size limit")
		writeError(w, http.StatusBadGateway, "api_error", "upstream response is too large")
		return
	}
	body = redact(body, access)
	if openaiList && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		body = codexManifestToOpenAIList(body)
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func streamable(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return ct == "" || strings.Contains(ct, "text/event-stream") || strings.HasPrefix(ct, "text/plain")
}

func (g *Gateway) responsesBase() string {
	if strings.TrimSpace(g.ResponsesURL) != "" {
		return g.ResponsesURL
	}
	return defaultResponsesURL
}

func (g *Gateway) modelsEndpoint(r *http.Request) (string, error) {
	base := g.ModelsURL
	if strings.TrimSpace(base) == "" {
		base = defaultModelsURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	clientVersion := strings.TrimSpace(r.URL.Query().Get("client_version"))
	if clientVersion == "" {
		clientVersion = oauth.ClientVersion()
	}
	q.Set("client_version", clientVersion)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func bearer(r *http.Request) (string, error) {
	value := r.Header.Get("Authorization")
	const scheme = "Bearer "
	if len(value) < len(scheme) || !strings.EqualFold(value[:len(scheme)], scheme) {
		return "", errors.New("missing bearer token")
	}
	tokenValue := strings.TrimSpace(value[len(scheme):])
	if tokenValue == "" || strings.ContainsAny(tokenValue, " \r\n") {
		return "", errors.New("missing bearer token")
	}
	return tokenValue, nil
}

func rejectedQueryCredential(w http.ResponseWriter, r *http.Request) bool {
	q := r.URL.Query()
	for _, name := range []string{"api_key", "key", "access_token"} {
		if q.Has(name) {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "API keys in the query string are not accepted")
			return true
		}
	}
	return false
}

func readLimited(w http.ResponseWriter, body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "request body could not be read")
		return nil, err
	}
	if int64(len(data)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body is too large")
		return nil, errors.New("body too large")
	}
	return data, nil
}

var passthroughHeaders = map[string]struct{}{
	"accept-language":         {},
	"conversation_id":         {},
	"session_id":              {},
	"x-codex-beta-features":   {},
	"x-codex-installation-id": {},
	"x-codex-turn-state":      {},
	"x-codex-turn-metadata":   {},
	"x-codex-window-id":       {},
}

func copyClientHeaders(src, dst http.Header) {
	for key, values := range src {
		if _, ok := passthroughHeaders[strings.ToLower(key)]; !ok {
			continue
		}
		for _, value := range values {
			if value == "" || strings.ContainsAny(value, "\r\n") {
				continue
			}
			dst.Add(key, value)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		if lower == "content-length" || lower == "connection" || lower == "keep-alive" || lower == "transfer-encoding" || lower == "proxy-authenticate" || lower == "proxy-authorization" || lower == "te" || lower == "trailer" || lower == "upgrade" || lower == "authorization" || lower == "set-cookie" || lower == "www-authenticate" {
			continue
		}
		forward := lower == "content-type" || lower == "cache-control" || lower == "etag" || lower == "x-request-id" || strings.HasPrefix(lower, "x-codex-")
		if !forward {
			continue
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				continue
			}
			dst.Add(key, value)
		}
	}
}

func streamCopy(w http.ResponseWriter, src io.Reader) error {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func redact(body []byte, secrets ...string) []byte {
	for _, secret := range secrets {
		if len(secret) < 12 {
			continue
		}
		body = bytes.ReplaceAll(body, []byte(secret), []byte("[redacted]"))
	}
	return body
}

func writeError(w http.ResponseWriter, status int, kind, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"type":    kind,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
