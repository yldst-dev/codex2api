package admin

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"codex-gateway/internal/apikey"
	"codex-gateway/internal/config"
	"codex-gateway/internal/gateway"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/store"
	"codex-gateway/internal/token"
)

//go:embed all:dist
var distFS embed.FS

const (
	sessionTTL     = 12 * time.Hour
	loginTTL       = 10 * time.Minute
	failureWindow  = 5 * time.Minute
	maxFailures    = 10
	maxRequestBody = 64 << 10
	contentPolicy  = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

type Server struct {
	Store        *store.Store
	Tokens       *token.Manager
	Keys         *apikey.Service
	Gateway      *gateway.Runner
	OAuth        *oauth.Client
	DataDir      string
	Listen       string
	ListenLocked bool
	AdminListen  string
	Log          *slog.Logger

	mu       sync.Mutex
	sessions map[string]session
	failures []time.Time
	login    *pendingLogin
	setupMu  sync.Mutex
	listenMu sync.Mutex
}

type session struct {
	stamp   string
	expires time.Time
}

type pendingLogin struct {
	flow     *oauth.Flow
	url      string
	callback bool
	expires  time.Time
	err      string
	cancel   context.CancelFunc
	done     chan struct{}
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(s.logger().Handler(), slog.LevelWarn),
	}
	errCh := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		s.cancelLogin()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", staticHandler())
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/login", s.signIn)
	mux.HandleFunc("POST /api/logout", s.authed(s.signOut))
	mux.HandleFunc("POST /api/password", s.authed(s.changePassword))
	mux.HandleFunc("POST /api/oauth/start", s.authed(s.oauthStart))
	mux.HandleFunc("POST /api/oauth/callback", s.authed(s.oauthCallback))
	mux.HandleFunc("POST /api/oauth/cancel", s.authed(s.oauthCancel))
	mux.HandleFunc("POST /api/oauth/refresh", s.authed(s.oauthRefresh))
	mux.HandleFunc("POST /api/oauth/logout", s.authed(s.oauthLogout))
	mux.HandleFunc("POST /api/keys", s.authed(s.createKey))
	mux.HandleFunc("POST /api/keys/{id}/rotate", s.authed(s.rotateKey))
	mux.HandleFunc("POST /api/keys/{id}/revoke", s.authed(s.revokeKey))
	mux.HandleFunc("POST /api/listen", s.authed(s.setListen))
	return s.guard(mux)
}

func staticHandler() http.Handler {
	app, err := fs.Sub(distFS, "dist/app")
	if err == nil {
		if _, err = fs.Stat(app, "index.html"); err == nil {
			return http.FileServerFS(app)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("관리 화면이 이 실행 파일에 들어 있지 않습니다. web 폴더에서 npm run build를 실행한 뒤 다시 빌드해 주세요.\n"))
	})
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if !config.IsLoopbackHost(host) {
			http.Error(w, "Forbidden host.", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !sameOrigin(r) {
				writeError(w, http.StatusForbidden, "다른 사이트에서 보낸 요청은 받지 않습니다.")
				return
			}
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "JSON 요청만 받습니다.")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	return origin == "" || origin == "http://"+r.Host
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.session(r); !ok {
			writeError(w, http.StatusUnauthorized, "다시 로그인해 주세요.")
			return
		}
		next(w, r)
	}
}

func (s *Server) session(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return "", false
	}
	stamp, err := s.Store.Meta(r.Context(), MetaPassword)
	if err != nil || stamp == "" {
		return "", false
	}
	id := sessionID(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, found := s.sessions[id]
	if !found || time.Now().After(sess.expires) || sess.stamp != stamp {
		delete(s.sessions, id)
		return "", false
	}
	return id, true
}

func (s *Server) newSession(stamp string) (string, error) {
	tok, err := randomHex(32)
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]session{}
	}
	for id, sess := range s.sessions {
		if now.After(sess.expires) || sess.stamp != stamp {
			delete(s.sessions, id)
		}
	}
	s.sessions[sessionID(tok)] = session{stamp: stamp, expires: now.Add(sessionTTL)}
	return tok, nil
}

func sessionID(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func (s *Server) allowAttempt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-failureWindow)
	kept := s.failures[:0]
	for _, at := range s.failures {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	s.failures = kept
	return len(kept) < maxFailures
}

func (s *Server) recordFailure() {
	s.mu.Lock()
	s.failures = append(s.failures, time.Now())
	s.mu.Unlock()
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	set, err := PasswordSet(ctx, s.Store)
	if err != nil {
		s.internal(w, err)
		return
	}
	if _, ok := s.session(r); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "setup_required": !set})
		return
	}
	acc, err := s.Tokens.Account(ctx)
	if err != nil {
		s.internal(w, err)
		return
	}
	keys, err := s.Keys.List(ctx)
	if err != nil {
		s.internal(w, err)
		return
	}
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		items = append(items, keyJSON(key))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"account":       accountJSON(acc),
		"login":         s.loginJSON(),
		"keys":          items,
		"gateway": map[string]any{
			"listen":  s.listen(),
			"running": s.Gateway.Addr() != "",
			"locked":  s.ListenLocked,
		},
		"admin_listen": s.AdminListen,
		"data_dir":     s.DataDir,
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !s.allowAttempt() {
		writeError(w, http.StatusTooManyRequests, "시도가 너무 많습니다. 5분 뒤에 다시 해 주세요.")
		return
	}
	if err := validatePassword(body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	set, err := PasswordSet(r.Context(), s.Store)
	if err != nil {
		s.internal(w, err)
		return
	}
	if set {
		writeError(w, http.StatusConflict, "비밀번호가 이미 정해져 있습니다. 로그인해 주세요.")
		return
	}
	if !checkSetupToken(s.DataDir, body.Token) {
		s.recordFailure()
		writeError(w, http.StatusForbidden, "설정 링크가 맞지 않거나 이미 사용되었습니다.")
		return
	}
	if !s.issuePassword(w, r, body.Password) {
		return
	}
	if err := os.Remove(setupTokenPath(s.DataDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.logger().Warn("remove setup token", "err", err)
	}
}

func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !s.allowAttempt() {
		writeError(w, http.StatusTooManyRequests, "시도가 너무 많습니다. 5분 뒤에 다시 해 주세요.")
		return
	}
	stored, err := s.Store.Meta(r.Context(), MetaPassword)
	if err != nil {
		s.internal(w, err)
		return
	}
	if stored == "" {
		writeError(w, http.StatusConflict, "아직 비밀번호가 없습니다. 설정 링크로 먼저 정해 주세요.")
		return
	}
	if !checkPassword(stored, body.Password) {
		s.recordFailure()
		writeError(w, http.StatusUnauthorized, "비밀번호가 맞지 않습니다.")
		return
	}
	tok, err := s.newSession(stored)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.session(r); ok {
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !s.allowAttempt() {
		writeError(w, http.StatusTooManyRequests, "시도가 너무 많습니다. 5분 뒤에 다시 해 주세요.")
		return
	}
	stored, err := s.Store.Meta(r.Context(), MetaPassword)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !checkPassword(stored, body.Current) {
		s.recordFailure()
		writeError(w, http.StatusUnauthorized, "현재 비밀번호가 맞지 않습니다.")
		return
	}
	if err := validatePassword(body.Next); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.issuePassword(w, r, body.Next)
}

func (s *Server) issuePassword(w http.ResponseWriter, r *http.Request, pw string) bool {
	hashed, err := hashPassword(pw)
	if err != nil {
		s.internal(w, err)
		return false
	}
	if err := s.Store.SetMeta(r.Context(), MetaPassword, hashed); err != nil {
		s.internal(w, err)
		return false
	}
	tok, err := s.newSession(hashed)
	if err != nil {
		s.internal(w, err)
		return false
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
	return true
}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	flow, authURL, err := oauth.StartFlow(s.OAuth)
	if err != nil {
		s.internal(w, err)
		return
	}
	s.cancelLogin()
	ctx, cancel := context.WithTimeout(context.Background(), loginTTL)
	p := &pendingLogin{
		flow:    flow,
		url:     authURL,
		expires: time.Now().Add(loginTTL),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	ln, lnErr := oauth.ListenCallback()
	p.callback = lnErr == nil
	if lnErr != nil {
		s.logger().Warn("oauth callback listener", "err", lnErr)
		close(p.done)
	}
	s.mu.Lock()
	s.login = p
	s.mu.Unlock()
	if ln != nil {
		go s.awaitCallback(ctx, p, ln)
	}
	writeJSON(w, http.StatusOK, map[string]any{"login": s.loginJSON()})
}

func (s *Server) awaitCallback(ctx context.Context, p *pendingLogin, ln net.Listener) {
	defer close(p.done)
	tok, err := p.flow.ServeCallback(ctx, ln)
	if err != nil {
		if ctx.Err() == nil {
			s.failLogin(p, err)
		}
		return
	}
	if _, err := s.Tokens.SaveLogin(context.Background(), tok); err != nil {
		s.failLogin(p, err)
		return
	}
	s.finishLogin(p)
}

func (s *Server) failLogin(p *pendingLogin, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.callback = false
	p.err = err.Error()
}

func (s *Server) finishLogin(p *pendingLogin) {
	s.mu.Lock()
	if s.login == p {
		s.login = nil
	}
	s.mu.Unlock()
	p.cancel()
}

func (s *Server) cancelLogin() {
	s.mu.Lock()
	p := s.login
	s.login = nil
	s.mu.Unlock()
	if p == nil {
		return
	}
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
	}
}

func (s *Server) currentLogin() *pendingLogin {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.login != nil && time.Now().After(s.login.expires) {
		s.login = nil
	}
	return s.login
}

func (s *Server) loginJSON() map[string]any {
	p := s.currentLogin()
	if p == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"url":        p.url,
		"callback":   p.callback,
		"expires_at": p.expires.UTC().Format(time.RFC3339),
		"error":      p.err,
	}
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &body) {
		return
	}
	p := s.currentLogin()
	if p == nil {
		writeError(w, http.StatusConflict, "진행 중인 로그인이 없습니다. 로그인을 다시 시작해 주세요.")
		return
	}
	tok, err := p.flow.ExchangeCallback(r.Context(), body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	acc, err := s.Tokens.SaveLogin(r.Context(), tok)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.finishLogin(p)
	writeJSON(w, http.StatusOK, map[string]any{"account": accountJSON(acc)})
}

func (s *Server) oauthCancel(w http.ResponseWriter, r *http.Request) {
	s.cancelLogin()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) oauthRefresh(w http.ResponseWriter, r *http.Request) {
	acc, err := s.Tokens.ForceRefresh(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountJSON(acc)})
}

func (s *Server) oauthLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.Tokens.Logout(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		body.Name = "default"
	}
	created, err := s.Keys.Create(r.Context(), body.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, createdJSON(created))
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	created, err := s.Keys.Rotate(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, createdJSON(created))
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	if err := s.Keys.Revoke(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setListen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Listen string `json:"listen"`
	}
	if !decode(w, r, &body) {
		return
	}
	if s.ListenLocked {
		writeError(w, http.StatusConflict, config.EnvListen+" 환경 변수가 수신 주소를 고정하고 있습니다. 서비스 설정에서 그 값을 지워야 여기서 바꿀 수 있습니다.")
		return
	}
	addr := strings.TrimSpace(body.Listen)
	if err := config.ValidateListen(addr); err != nil {
		writeError(w, http.StatusBadRequest, "수신 주소는 127.0.0.1:8080처럼 호스트와 포트를 함께 적어 주세요.")
		return
	}
	if addr == s.AdminListen {
		writeError(w, http.StatusBadRequest, "관리 화면과 같은 주소는 쓸 수 없습니다.")
		return
	}
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	old := s.Gateway.Addr()
	if err := s.Gateway.Start(addr); err != nil {
		writeError(w, http.StatusBadRequest, "이 주소로 열지 못했습니다: "+err.Error())
		return
	}
	if err := s.Store.SetMeta(r.Context(), config.MetaListen, addr); err != nil {
		if old != "" {
			_ = s.Gateway.Start(old)
		}
		s.internal(w, err)
		return
	}
	s.mu.Lock()
	s.Listen = addr
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"listen": addr, "running": true})
}

func (s *Server) listen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Listen
}

func (s *Server) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.logger().Error("admin request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "서버에서 처리하지 못했습니다. 로그를 확인해 주세요.")
}

func accountJSON(acc *store.OAuthAccount) map[string]any {
	if acc == nil {
		return nil
	}
	return map[string]any{
		"email":      acc.Email,
		"account_id": acc.AccountID,
		"plan_type":  acc.PlanType,
		"status":     acc.Status,
		"ready":      acc.Status == store.OAuthActive && acc.RefreshToken != "",
		"expires_at": acc.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func keyJSON(key store.APIKey) map[string]any {
	item := map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"prefix":     key.Prefix,
		"status":     key.Status,
		"created_at": key.CreatedAt.UTC().Format(time.RFC3339),
	}
	if key.LastUsedAt != nil {
		item["last_used_at"] = key.LastUsedAt.UTC().Format(time.RFC3339)
	}
	if key.RevokedAt != nil {
		item["revoked_at"] = key.RevokedAt.UTC().Format(time.RFC3339)
	}
	return item
}

func createdJSON(c apikey.Created) map[string]string {
	return map[string]string{"id": c.ID, "name": c.Name, "prefix": c.Prefix, "key": c.Key}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "요청 형식이 맞지 않습니다.")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
