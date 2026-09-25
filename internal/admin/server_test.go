package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"codex-gateway/internal/apikey"
	"codex-gateway/internal/config"
	"codex-gateway/internal/gateway"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/store"
	"codex-gateway/internal/token"
)

type harness struct {
	t      *testing.T
	srv    *Server
	h      http.Handler
	dir    string
	runner *gateway.Runner
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	passwordIterations = 1000
	dir := t.TempDir()
	st, err := store.Open(dir, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	keys := apikey.NewService(st, bytes.Repeat([]byte{7}, 32), nil)
	tokens := token.NewManager(st, oauth.NewClient(""))
	runner := &gateway.Runner{Handler: (&gateway.Gateway{Tokens: tokens, Keys: keys}).Handler()}
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	srv := &Server{
		Store:       st,
		Tokens:      tokens,
		Keys:        keys,
		Gateway:     runner,
		DataDir:     dir,
		Listen:      "127.0.0.1:0",
		AdminListen: "127.0.0.1:8081",
	}
	return &harness{t: t, srv: srv, h: srv.Handler(), dir: dir, runner: runner}
}

func (h *harness) do(method, path, bearer string, body any, mutate ...func(*http.Request)) (*httptest.ResponseRecorder, map[string]any) {
	h.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:8081"+path, reader)
	req.Host = "127.0.0.1:8081"
	req.RemoteAddr = "127.0.0.1:50000"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:8081")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for _, fn := range mutate {
		fn(req)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (h *harness) setup(password string) string {
	h.t.Helper()
	tok, err := EnsureSetupToken(context.Background(), h.srv.Store, h.dir)
	if err != nil || tok == "" {
		h.t.Fatalf("setup token %q %v", tok, err)
	}
	rec, out := h.do(http.MethodPost, "/api/setup", "", map[string]string{"token": tok, "password": password})
	if rec.Code != http.StatusOK {
		h.t.Fatalf("setup %d %s", rec.Code, rec.Body.String())
	}
	return out["token"].(string)
}

func TestGuardRejectsForeignHostOriginAndContentType(t *testing.T) {
	h := newHarness(t)
	rec, _ := h.do(http.MethodGet, "/api/state", "", nil, func(r *http.Request) { r.Host = "evil.example:8081" })
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign host = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "x"}, func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross origin = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "x"}, func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Del("Origin")
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross site = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "x"}, func(r *http.Request) {
		r.Header.Set("Content-Type", "text/plain")
	})
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodGet, "/api/state", "", nil, func(r *http.Request) { r.Host = "localhost:9000" })
	if rec.Code != http.StatusOK {
		t.Fatalf("tunnel host = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("csp = %q", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestSetupLoginAndSessions(t *testing.T) {
	h := newHarness(t)
	_, out := h.do(http.MethodGet, "/api/state", "", nil)
	if out["setup_required"] != true || out["authenticated"] != false {
		t.Fatalf("state = %v", out)
	}
	if _, err := EnsureSetupToken(context.Background(), h.srv.Store, h.dir); err != nil {
		t.Fatal(err)
	}
	rec, _ := h.do(http.MethodPost, "/api/setup", "", map[string]string{"token": strings.Repeat("0", 64), "password": "correct horse"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong setup token = %d", rec.Code)
	}
	session := h.setup("correct horse")
	if _, err := os.Stat(setupTokenPath(h.dir)); !os.IsNotExist(err) {
		t.Fatalf("setup token kept: %v", err)
	}
	rec, _ = h.do(http.MethodPost, "/api/setup", "", map[string]string{"token": "x", "password": "another one"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup = %d", rec.Code)
	}
	if tok, _ := EnsureSetupToken(context.Background(), h.srv.Store, h.dir); tok != "" {
		t.Fatal("setup token issued after password was set")
	}

	_, out = h.do(http.MethodGet, "/api/state", session, nil)
	if out["authenticated"] != true {
		t.Fatalf("authed state = %v", out)
	}
	rec, _ = h.do(http.MethodPost, "/api/keys", "", map[string]string{"name": "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer = %d", rec.Code)
	}

	rec, _ = h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "wrong password"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d", rec.Code)
	}
	rec, out = h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "correct horse"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d", rec.Code)
	}
	second := out["token"].(string)

	rec, out = h.do(http.MethodPost, "/api/password", session, map[string]string{"current": "correct horse", "next": "battery staple"})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password = %d %s", rec.Code, rec.Body.String())
	}
	fresh := out["token"].(string)
	for _, old := range []string{session, second} {
		rec, _ = h.do(http.MethodPost, "/api/logout", old, map[string]string{})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("old session still valid: %d", rec.Code)
		}
	}
	rec, _ = h.do(http.MethodPost, "/api/logout", fresh, map[string]string{})
	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodPost, "/api/logout", fresh, map[string]string{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d", rec.Code)
	}

	link, err := ResetPassword(context.Background(), h.srv.Store, h.dir)
	if err != nil || link == "" {
		t.Fatalf("reset %q %v", link, err)
	}
	_, out = h.do(http.MethodGet, "/api/state", "", nil)
	if out["setup_required"] != true {
		t.Fatalf("after reset = %v", out)
	}
}

func TestLoginRateLimit(t *testing.T) {
	h := newHarness(t)
	h.setup("correct horse")
	for i := 0; i < maxFailures; i++ {
		h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "nope nope"})
	}
	rec, _ := h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "correct horse"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after failures = %d", rec.Code)
	}
}

func TestKeysThroughAdmin(t *testing.T) {
	h := newHarness(t)
	session := h.setup("correct horse")
	rec, created := h.do(http.MethodPost, "/api/keys", session, map[string]string{"name": "laptop"})
	if rec.Code != http.StatusOK || !strings.HasPrefix(created["key"].(string), apikey.Prefix) {
		t.Fatalf("create = %d %v", rec.Code, created)
	}
	id := created["id"].(string)
	rec, _ = h.do(http.MethodGet, "/api/state", session, nil)
	if strings.Contains(rec.Body.String(), created["key"].(string)) {
		t.Fatal("state exposed the plaintext key")
	}
	rec, rotated := h.do(http.MethodPost, "/api/keys/"+id+"/rotate", session, map[string]string{})
	if rec.Code != http.StatusOK || rotated["key"] == created["key"] {
		t.Fatalf("rotate = %d %v", rec.Code, rotated)
	}
	rec, _ = h.do(http.MethodPost, "/api/keys/"+id+"/revoke", session, map[string]string{})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodPost, "/api/oauth/callback", session, map[string]string{"url": "http://localhost:1455/auth/callback?code=a&state=b"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("callback without login = %d", rec.Code)
	}
}

func TestListenAppliesWithoutRestart(t *testing.T) {
	h := newHarness(t)
	session := h.setup("correct horse")
	first := freeAddr(t)
	if err := h.runner.Start(first); err != nil {
		t.Fatal(err)
	}
	second := freeAddr(t)
	rec, _ := h.do(http.MethodPost, "/api/listen", session, map[string]string{"listen": second})
	if rec.Code != http.StatusOK {
		t.Fatalf("listen = %d %s", rec.Code, rec.Body.String())
	}
	if h.runner.Addr() != second {
		t.Fatalf("runner addr = %s", h.runner.Addr())
	}
	resp, err := http.Get("http://" + second + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	stored, _ := h.srv.Store.Meta(context.Background(), config.MetaListen)
	if stored != second {
		t.Fatalf("stored listen = %q", stored)
	}

	rec, _ = h.do(http.MethodPost, "/api/listen", session, map[string]string{"listen": "not an address"})
	if rec.Code != http.StatusBadRequest || h.runner.Addr() != second {
		t.Fatalf("bad listen = %d addr %s", rec.Code, h.runner.Addr())
	}
	h.srv.ListenLocked = true
	rec, _ = h.do(http.MethodPost, "/api/listen", session, map[string]string{"listen": first})
	if rec.Code != http.StatusConflict {
		t.Fatalf("locked listen = %d", rec.Code)
	}
}

func TestPasswordHash(t *testing.T) {
	passwordIterations = 1000
	hashed, err := hashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(hashed, "correct horse") || checkPassword(hashed, "correct horsE") || checkPassword("garbage", "x") {
		t.Fatal("password check")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
