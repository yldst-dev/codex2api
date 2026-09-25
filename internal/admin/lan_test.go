package admin

import (
	"net/http"
	"testing"

	"codex-gateway/internal/config"
)

func fromLAN(remote, host string) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = remote
		r.Host = host
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", "http://"+host)
		}
	}
}

func TestLANClosedByDefault(t *testing.T) {
	h := newHarness(t)
	rec, _ := h.do(http.MethodGet, "/api/state", "", nil, fromLAN("192.168.0.23:50000", "127.0.0.1:8081"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("LAN client with default access = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodGet, "/api/state", "", nil, fromLAN("127.0.0.1:50000", "192.168.0.9:8081"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("LAN host with default access = %d", rec.Code)
	}
}

func TestLANOpenedWithAllowList(t *testing.T) {
	h := newHarness(t)
	access, err := config.ParseAdminAllow("192.168.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	h.srv.Access = access
	h.h = h.srv.Handler()
	h.setup("correct horse")

	lan := fromLAN("192.168.0.23:50000", "192.168.0.9:8081")
	rec, out := h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "correct horse"}, lan)
	if rec.Code != http.StatusOK {
		t.Fatalf("LAN login = %d %s", rec.Code, rec.Body.String())
	}
	session := out["token"].(string)
	rec, out = h.do(http.MethodGet, "/api/state", session, nil, lan)
	if rec.Code != http.StatusOK || out["admin_allow"] != "192.168.0.0/24" {
		t.Fatalf("LAN state = %d %v", rec.Code, out["admin_allow"])
	}

	cases := []struct {
		name         string
		remote, host string
	}{
		{"client outside range", "10.0.0.5:50000", "192.168.0.9:8081"},
		{"public client", "203.0.113.7:50000", "192.168.0.9:8081"},
		{"host name for rebinding", "192.168.0.23:50000", "gateway.evil.test:8081"},
		{"host outside range", "192.168.0.23:50000", "192.168.1.9:8081"},
	}
	for _, c := range cases {
		rec, _ := h.do(http.MethodGet, "/api/state", "", nil, fromLAN(c.remote, c.host))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s = %d", c.name, rec.Code)
		}
	}
	rec, _ = h.do(http.MethodPost, "/api/login", "", map[string]string{"password": "correct horse"}, func(r *http.Request) {
		r.RemoteAddr = "192.168.0.23:50000"
		r.Host = "192.168.0.9:8081"
		r.Header.Set("Origin", "http://evil.test")
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin LAN post = %d", rec.Code)
	}
}

func TestSetupURLForWildcardListen(t *testing.T) {
	if got := SetupURL("0.0.0.0:8081", "abc"); got != "http://127.0.0.1:8081/#setup=abc" {
		t.Fatalf("wildcard = %s", got)
	}
	if got := SetupURL("192.168.0.9:8081", "abc"); got != "http://192.168.0.9:8081/#setup=abc" {
		t.Fatalf("lan = %s", got)
	}
}
