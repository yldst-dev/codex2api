package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPKCEAndAuthorizationURL(t *testing.T) {
	flow, raw, err := StartFlow(NewClient(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.State) != 64 {
		t.Fatalf("state length %d", len(flow.State))
	}
	if len(flow.Verifier) != 128 {
		t.Fatalf("verifier length %d", len(flow.Verifier))
	}
	if CodeChallenge(flow.Verifier) == flow.Verifier {
		t.Fatal("challenge must differ from verifier")
	}
	if pad := strings.TrimRight(CodeChallenge("abc"), "="); strings.Contains(pad, "=") {
		t.Fatal("challenge must be unpadded")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != ClientID {
		t.Fatalf("client_id %s", q.Get("client_id"))
	}
	if q.Get("code_challenge") != CodeChallenge(flow.Verifier) {
		t.Fatal("challenge mismatch")
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("codex_cli_simplified_flow") != "true" {
		t.Fatalf("query = %s", q.Encode())
	}
	if q.Get("id_token_add_organizations") != "true" || q.Get("redirect_uri") != RedirectURI {
		t.Fatalf("query = %s", q.Encode())
	}
	if q.Get("client_secret") != "" {
		t.Fatal("public client must not send a secret")
	}
}

func TestStateMismatch(t *testing.T) {
	flow, _, err := StartFlow(NewClient("http://127.0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = flow.ExchangeCallback(context.Background(), "http://localhost:1455/auth/callback?code=abc&state=different")
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("err = %v", err)
	}
}

func TestExchangeAndRefresh(t *testing.T) {
	var got url.Values
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(body))
		gotHeaders = r.Header.Clone()
		if got.Get("grant_type") == "authorization_code" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-1",
				"refresh_token": "refresh-1",
				"id_token":      testJWT(t, time.Now().Add(time.Hour).Unix()),
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
			return
		}
		if got.Get("refresh_token") != "refresh-1" {
			http.Error(w, "wrong refresh", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-2",
			"refresh_token": "refresh-2",
			"expires_in":    1200,
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	flow, _, err := StartFlow(client)
	if err != nil {
		t.Fatal(err)
	}
	callback := "http://localhost:1455/auth/callback?code=the-code&state=" + flow.State
	tok, err := flow.ExchangeCallback(context.Background(), callback)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("client_secret") != "" || got.Get("code") != "the-code" || got.Get("code_verifier") != flow.Verifier {
		t.Fatalf("form = %v", got)
	}
	if got.Get("client_id") != ClientID || got.Get("redirect_uri") != RedirectURI {
		t.Fatalf("form = %v", got)
	}
	if gotHeaders.Get("originator") != Originator || !strings.Contains(gotHeaders.Get("User-Agent"), "codex-tui/"+Version) {
		t.Fatalf("headers = %v", gotHeaders)
	}
	if tok.Email != "user@example.com" || tok.AccountID != "acc_123" || tok.PlanType != "plus" {
		t.Fatalf("token identity = %+v", tok)
	}
	refreshed, err := client.Refresh(context.Background(), tok.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("grant_type") != "refresh_token" || got.Get("scope") != RefreshScope || got.Get("client_secret") != "" {
		t.Fatalf("refresh form = %v", got)
	}
	if refreshed.AccessToken != "access-2" || refreshed.RefreshToken != "refresh-2" {
		t.Fatalf("refreshed = %+v", refreshed)
	}
}

func TestInvalidGrantClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token leaked ` + strings.Repeat("x", 80) + `"}`))
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL).Refresh(context.Background(), "refresh-secret-value")
	var endpoint *EndpointError
	if err == nil || !errorsAs(err, &endpoint) || endpoint.Temporary || endpoint.Code != "invalid_grant" {
		t.Fatalf("err = %#v", err)
	}
	if strings.Contains(err.Error(), "refresh-secret-value") || strings.Contains(err.Error(), "xxxxx") {
		t.Fatalf("error leaked secret: %s", err.Error())
	}
}

func TestNetworkErrorIsTemporary(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	client.HTTP.Timeout = 200 * time.Millisecond
	_, err := client.Refresh(context.Background(), "refresh-token")
	var endpoint *EndpointError
	if err == nil || !errorsAs(err, &endpoint) || !endpoint.Temporary {
		t.Fatalf("err = %#v", err)
	}
}

func TestParseIDTokenAudienceString(t *testing.T) {
	raw := jwt(t, map[string]any{
		"email": "a@example.com",
		"aud":   "app",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acc",
			"chatgpt_plan_type":  "free",
		},
	})
	info, err := ParseIDToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "a@example.com" || info.ChatGPTAccountID != "acc" || info.PlanType != "free" {
		t.Fatalf("info = %+v", info)
	}
}

func TestHeaderVersionFloor(t *testing.T) {
	if HeaderVersion("0.1.0") != Version {
		t.Fatal("old version should fall back")
	}
	if HeaderVersion("0.146.0") != "0.146.0" {
		t.Fatal("current version should pass")
	}
	if HeaderVersion("not a version") != Version {
		t.Fatal("invalid version should fall back")
	}
}

func testJWT(t *testing.T, exp int64) string {
	t.Helper()
	return jwt(t, map[string]any{
		"email": "user@example.com",
		"exp":   exp,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acc_123",
			"chatgpt_user_id":    "user_123",
			"user_id":            "user_123",
			"chatgpt_plan_type":  "plus",
			"organizations":      []map[string]any{{"id": "org_1", "is_default": true}},
		},
	})
}

func jwt(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func errorsAs(err error, target **EndpointError) bool {
	if err == nil {
		return false
	}
	endpoint, ok := err.(*EndpointError)
	if !ok {
		return false
	}
	*target = endpoint
	return true
}
