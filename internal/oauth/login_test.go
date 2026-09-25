package oauth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServeCallbackIgnoresForeignState(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-1",
			"refresh_token": "refresh-1",
			"id_token":      testJWT(t, time.Now().Add(time.Hour).Unix()),
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	flow, _, err := StartFlow(NewClient(tokenSrv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String() + "/auth/callback"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan *Token, 1)
	go func() {
		tok, err := flow.ServeCallback(ctx, ln)
		if err != nil {
			t.Error(err)
		}
		done <- tok
	}()

	resp, err := http.Get(base + "?code=x&state=forged")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged state = %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "?code=real&state=" + flow.State)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("real callback = %d", resp.StatusCode)
	}
	if tok := <-done; tok == nil || tok.AccessToken != "access-1" {
		t.Fatalf("token = %+v", tok)
	}
}
