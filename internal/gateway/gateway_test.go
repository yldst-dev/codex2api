package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-gateway/internal/apikey"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/store"
	"codex-gateway/internal/token"
)

func TestGatewayProxyAuthStreamAndErrors(t *testing.T) {
	var mu sync.Mutex
	var saw map[string]string
	var sawBody []byte
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		saw = map[string]string{
			"authorization": r.Header.Get("Authorization"),
			"account":       r.Header.Get("chatgpt-account-id"),
			"originator":    r.Header.Get("originator"),
			"version":       r.Header.Get("version"),
			"beta":          r.Header.Get("OpenAI-Beta"),
			"session":       r.Header.Get("session_id"),
			"accept":        r.Header.Get("Accept"),
			"ua":            r.Header.Get("User-Agent"),
			"host":          r.Host,
			"path":          r.URL.Path,
			"features":      r.Header.Get("x-codex-beta-features"),
		}
		sawBody = append([]byte(nil), body...)
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"slug":"from-upstream"}]}`))
		case strings.Contains(r.URL.Path, "/compact"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad compact"}`))
		case strings.Contains(string(body), `"stream":true`):
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: one\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-release
			_, _ = w.Write([]byte("data: two\n\n"))
		case strings.Contains(string(body), `"explode":true`):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"echo":"`+r.Header.Get("Authorization")+`"}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("x-codex-turn-state", "turn-1")
			if r.Header.Get("Authorization") == "" || strings.Contains(r.Header.Get("Authorization"), "cg_") {
				w.WriteHeader(http.StatusBadRequest)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer upstream.Close()

	st, svc, mgr := testStack(t)
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "oauth-access-token-value",
		RefreshToken: "oauth-refresh-token-value",
		ExpiresAt:    time.Now().Add(time.Hour),
		AccountID:    "acc_123",
		Email:        "user@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.Create(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	gw := &Gateway{
		Tokens:       mgr,
		Keys:         svc,
		ResponsesURL: upstream.URL + "/backend-api/codex/responses",
		ModelsURL:    upstream.URL + "/backend-api/codex/models",
		Log:          slog.New(slog.NewTextHandler(&logs, nil)),
	}
	server := httptest.NewServer(gw.Handler())
	defer server.Close()

	if resp := get(t, server.URL+"/health", ""); resp.status != 200 || !strings.Contains(resp.body, `"oauth":true`) {
		t.Fatalf("health = %d %s", resp.status, resp.body)
	}
	if resp := post(t, server.URL+"/v1/responses", "", `{"model":"m","input":"hi"}`); resp.status != 401 {
		t.Fatalf("missing auth = %d %s", resp.status, resp.body)
	}
	if resp := post(t, server.URL+"/v1/responses", "Bearer cg_wrong_key_value", `{"model":"m","input":"hi"}`); resp.status != 401 {
		t.Fatalf("bad auth = %d %s", resp.status, resp.body)
	}
	if resp := post(t, server.URL+"/v1/responses?api_key="+created.Key, "Bearer "+created.Key, `{"model":"m","input":"hi"}`); resp.status != 400 {
		t.Fatalf("query key = %d %s", resp.status, resp.body)
	}

	resp := postHeader(t, server.URL+"/v1/responses", "Bearer "+created.Key, `{"model":"m","input":"hi","instructions":"keep me"}`, map[string]string{"session_id": "sess-1"})
	if resp.status != 200 || !strings.Contains(resp.body, `"ok":true`) {
		t.Fatalf("proxy = %d %s", resp.status, resp.body)
	}
	if resp.header.Get("x-codex-turn-state") != "turn-1" {
		t.Fatalf("turn state = %s", resp.header.Get("x-codex-turn-state"))
	}
	mu.Lock()
	got := saw
	gotBody := string(sawBody)
	mu.Unlock()
	if got["authorization"] != "Bearer oauth-access-token-value" {
		t.Fatalf("upstream auth = %s", got["authorization"])
	}
	if strings.Contains(got["authorization"], "cg_") || strings.Contains(gotBody, created.Key) {
		t.Fatal("gateway key reached upstream")
	}
	if got["account"] != "acc_123" || got["originator"] != oauth.Originator || got["version"] != oauth.Version {
		t.Fatalf("identity headers = %+v", got)
	}
	if got["beta"] != "responses=experimental" || got["session"] != "sess-1" || got["host"] != "chatgpt.com" {
		t.Fatalf("codex headers = %+v", got)
	}
	if got["ua"] != oauth.UserAgent || got["accept"] != "text/event-stream" || got["features"] != "remote_compaction_v2" {
		t.Fatalf("codex headers = %+v", got)
	}
	if gotBody != `{"model":"m","input":"hi","instructions":"keep me"}` {
		t.Fatalf("body changed: %s", gotBody)
	}
	if strings.Contains(resp.body, "oauth-access-token-value") || strings.Contains(resp.body, "oauth-refresh-token-value") {
		t.Fatal("oauth token returned to client")
	}

	injected := post(t, server.URL+"/v1/responses", "Bearer "+created.Key, `{"model":"m","input":"hi"}`)
	if injected.status != 200 {
		t.Fatalf("inject = %d %s", injected.status, injected.body)
	}
	mu.Lock()
	var parsed map[string]any
	if err := json.Unmarshal(sawBody, &parsed); err != nil {
		t.Fatal(err)
	}
	mu.Unlock()
	instructions, _ := parsed["instructions"].(string)
	if strings.TrimSpace(instructions) == "" {
		t.Fatal("instructions were not injected")
	}

	echo := post(t, server.URL+"/v1/responses", "Bearer "+created.Key, `{"model":"m","input":"hi","instructions":"keep","explode":true}`)
	if strings.Contains(echo.body, "oauth-access-token-value") || !strings.Contains(echo.body, "[redacted]") {
		t.Fatalf("echo = %s", echo.body)
	}

	compact := post(t, server.URL+"/backend-api/codex/responses/compact", "Bearer "+created.Key, `{"model":"m","input":"hi","instructions":"keep"}`)
	if compact.status != 400 || !strings.Contains(compact.body, "bad compact") {
		t.Fatalf("compact = %d %s", compact.status, compact.body)
	}
	mu.Lock()
	if saw["accept"] != "application/json" || !strings.HasSuffix(saw["path"], "/compact") {
		t.Fatalf("compact upstream = %+v", saw)
	}
	mu.Unlock()

	models := get(t, server.URL+"/v1/models?client_version=0.146.0", "Bearer "+created.Key)
	if models.status != 200 || !strings.Contains(models.body, `"id":"from-upstream"`) || !strings.Contains(models.body, `"object":"list"`) {
		t.Fatalf("models = %d %s", models.status, models.body)
	}
	rawModels := get(t, server.URL+"/models", "Bearer "+created.Key)
	if !strings.Contains(rawModels.body, `"slug":"from-upstream"`) {
		t.Fatalf("raw models = %s", rawModels.body)
	}

	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	streamBody := `{"model":"m","input":"hi","instructions":"keep","stream":true}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/responses", strings.NewReader(streamBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+created.Key)
	req.Header.Set("Content-Type", "application/json")
	streamResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()
	readDone := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := streamResp.Body.Read(buf)
		readDone <- string(buf[:n])
	}()
	select {
	case chunk := <-readDone:
		if !strings.Contains(chunk, "data: one") {
			t.Fatalf("chunk = %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream was buffered")
	}
	releaseOnce.Do(func() { close(release) })
	rest, _ := io.ReadAll(streamResp.Body)
	if !strings.Contains(string(rest), "data: two") {
		t.Fatalf("rest = %s", rest)
	}

	if err := svc.Revoke(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	revoked := post(t, server.URL+"/v1/responses", "Bearer "+created.Key, `{"model":"m","input":"hi"}`)
	if revoked.status != 401 {
		t.Fatalf("revoked = %d", revoked.status)
	}
	if strings.Contains(logs.String(), created.Key) || strings.Contains(logs.String(), "oauth-access-token-value") || strings.Contains(logs.String(), "oauth-refresh-token-value") {
		t.Fatalf("logs leaked secrets: %s", logs.String())
	}
	_ = st
}

func TestClientCancelAndUpstreamFailure(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer upstream.Close()
	_, svc, mgr := testStack(t)
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "oauth-access-token-value",
		RefreshToken: "oauth-refresh-token-value",
		ExpiresAt:    time.Now().Add(time.Hour),
		AccountID:    "acc_123",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.Create(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	gw := &Gateway{
		Tokens:       mgr,
		Keys:         svc,
		ResponsesURL: upstream.URL + "/backend-api/codex/responses",
	}
	server := httptest.NewServer(gw.Handler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"m","input":"hi","instructions":"keep"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+created.Key)
	done := make(chan struct{})
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was not called")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was not canceled")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client request did not finish")
	}

	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer fail.Close()
	gw.ResponsesURL = fail.URL + "/backend-api/codex/responses"
	gw.Client = fail.Client()
	server2 := httptest.NewServer(gw.Handler())
	defer server2.Close()
	resp := post(t, server2.URL+"/v1/responses", "Bearer "+created.Key, `{"model":"m","input":"hi","instructions":"keep"}`)
	if resp.status != http.StatusBadGateway || strings.Contains(resp.body, "oauth-access-token-value") {
		t.Fatalf("upstream 502 = %d %s", resp.status, resp.body)
	}
}

func TestOversizedUpstreamBodyIsRejected(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxUpstreamBody+1))
	}))
	defer upstream.Close()
	_, svc, mgr := testStack(t)
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "oauth-access-token-value",
		RefreshToken: "oauth-refresh-token-value",
		ExpiresAt:    time.Now().Add(time.Hour),
		AccountID:    "acc_123",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.Create(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	gw := &Gateway{Tokens: mgr, Keys: svc, ResponsesURL: upstream.URL + "/backend-api/codex/responses"}
	server := httptest.NewServer(gw.Handler())
	defer server.Close()
	resp := post(t, server.URL+"/v1/responses/compact", "Bearer "+created.Key, `{"model":"m","input":[],"instructions":"keep"}`)
	if resp.status != http.StatusBadGateway || !strings.Contains(resp.body, "too large") {
		t.Fatalf("oversized = %d %.200s", resp.status, resp.body)
	}
}

func TestMissingOAuth(t *testing.T) {
	_, svc, mgr := testStack(t)
	created, err := svc.Create(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	gw := &Gateway{Tokens: mgr, Keys: svc}
	server := httptest.NewServer(gw.Handler())
	defer server.Close()
	resp := post(t, server.URL+"/v1/responses", "Bearer "+created.Key, `{"model":"m","input":"hi"}`)
	if resp.status != http.StatusServiceUnavailable || !strings.Contains(resp.body, "codex-gateway auth login") {
		t.Fatalf("missing oauth = %d %s", resp.status, resp.body)
	}
}

func testStack(t *testing.T) (*store.Store, *apikey.Service, *token.Manager) {
	t.Helper()
	master := bytes.Repeat([]byte{3}, 32)
	st, err := store.Open(t.TempDir(), master)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, apikey.NewService(st, master, slog.New(slog.NewTextHandler(io.Discard, nil))), token.NewManager(st, oauth.NewClient("http://127.0.0.1"))
}

type recorded struct {
	status int
	body   string
	header http.Header
}

func post(t *testing.T, url, auth, body string) recorded {
	t.Helper()
	return postHeader(t, url, auth, body, nil)
}

func postHeader(t *testing.T, url, auth, body string, headers map[string]string) recorded {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return recorded{status: resp.StatusCode, body: string(data), header: resp.Header.Clone()}
}

func get(t *testing.T, url, auth string) recorded {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return recorded{status: resp.StatusCode, body: string(data), header: resp.Header.Clone()}
}
