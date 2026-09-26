package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-gateway/internal/oauth"
)

func TestResponsesStreamWithoutEventStreamContentType(t *testing.T) {
	for name, setType := range map[string]func(http.Header){
		"missing":    func(h http.Header) { h["Content-Type"] = nil },
		"text/plain": func(h http.Header) { h.Set("Content-Type", "text/plain; charset=utf-8") },
	} {
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				setType(w.Header())
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"delta\":\"one\"}\n\n"))
				w.(http.Flusher).Flush()
				<-release
				_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"))
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
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })

			req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/responses",
				strings.NewReader(`{"model":"m","input":[],"instructions":"keep","stream":true,"store":false}`))
			req.Header.Set("Authorization", "Bearer "+created.Key)
			req.Header.Set("Content-Type", "application/json")
			got := make(chan *http.Response, 1)
			go func() {
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
					close(got)
					return
				}
				got <- resp
			}()
			var resp *http.Response
			select {
			case resp = <-got:
			case <-time.After(2 * time.Second):
				t.Fatal("response headers were held until the upstream finished")
			}
			if resp == nil {
				t.FailNow()
			}
			defer resp.Body.Close()
			if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
				t.Fatalf("content type = %q", ct)
			}
			first := make(chan string, 1)
			go func() {
				buf := make([]byte, 256)
				n, _ := resp.Body.Read(buf)
				first <- string(buf[:n])
			}()
			select {
			case chunk := <-first:
				if !strings.Contains(chunk, `"delta":"one"`) {
					t.Fatalf("first chunk = %q", chunk)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("stream was buffered until the upstream finished")
			}
			releaseOnce.Do(func() { close(release) })
			rest, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(rest), "response.completed") {
				t.Fatalf("rest = %q", rest)
			}
		})
	}
}

func TestResponsesErrorsAndCompactAreNotStreamed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/compact") {
			w.Header()["Content-Type"] = nil
			_, _ = w.Write([]byte(`{"output":[]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"Store must be set to false"}`))
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

	bad := post(t, server.URL+"/v1/responses", "Bearer "+created.Key, `{"model":"m","input":[],"instructions":"keep","stream":true,"store":true}`)
	if bad.status != http.StatusBadRequest || !strings.Contains(bad.body, "Store must be set to false") || bad.header.Get("Content-Type") != "application/json" {
		t.Fatalf("upstream error = %d %q %q", bad.status, bad.header.Get("Content-Type"), bad.body)
	}
	compact := post(t, server.URL+"/v1/responses/compact", "Bearer "+created.Key, `{"model":"m","input":[],"instructions":"keep"}`)
	if compact.status != http.StatusOK || compact.body != `{"output":[]}` || strings.Contains(compact.header.Get("Content-Type"), "event-stream") {
		t.Fatalf("compact = %d %q %q", compact.status, compact.header.Get("Content-Type"), compact.body)
	}
}
