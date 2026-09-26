package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codex-gateway/internal/oauth"
)

func TestUpstreamHeadersFollowRuntimeClientVersion(t *testing.T) {
	t.Cleanup(oauth.ResetClientVersion)
	seen := make(chan http.Header, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer upstream.Close()
	_, svc, mgr := testStack(t)
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour), AccountID: "acc",
	}); err != nil {
		t.Fatal(err)
	}
	created, _ := svc.Create(context.Background(), "default")
	gw := &Gateway{Tokens: mgr, Keys: svc, ResponsesURL: upstream.URL + "/backend-api/codex/responses"}
	server := httptest.NewServer(gw.Handler())
	defer server.Close()
	body := `{"model":"m","input":[],"instructions":"keep","stream":true,"store":false}`

	post(t, server.URL+"/v1/responses", "Bearer "+created.Key, body)
	h := <-seen
	if h.Get("version") != oauth.Version || h.Get("User-Agent") != oauth.UserAgent {
		t.Fatalf("built-in headers = %q %q", h.Get("version"), h.Get("User-Agent"))
	}

	if !oauth.SetClientVersion("999.0.0") {
		t.Fatal("newer version was not applied")
	}
	post(t, server.URL+"/v1/responses", "Bearer "+created.Key, body)
	h = <-seen
	if h.Get("version") != "999.0.0" || h.Get("User-Agent") != "codex-tui/999.0.0 (Ubuntu 22.4.0; x86_64) xterm-256color" {
		t.Fatalf("runtime headers = %q %q", h.Get("version"), h.Get("User-Agent"))
	}
}
