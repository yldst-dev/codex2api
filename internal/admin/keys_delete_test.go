package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDeleteOnlyRevokedKeys(t *testing.T) {
	h := newHarness(t)
	session := h.setup("correct horse")

	_, active := h.do(http.MethodPost, "/api/keys", session, map[string]string{"name": "keep"})
	_, first := h.do(http.MethodPost, "/api/keys", session, map[string]string{"name": "old-1"})
	_, second := h.do(http.MethodPost, "/api/keys", session, map[string]string{"name": "old-2"})
	activeID, firstID, secondID := active["id"].(string), first["id"].(string), second["id"].(string)

	rec, _ := h.do(http.MethodPost, "/api/keys/"+activeID+"/delete", session, map[string]string{})
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete active = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodPost, "/api/keys/nope/delete", session, map[string]string{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d", rec.Code)
	}
	rec, _ = h.do(http.MethodPost, "/api/keys/"+firstID+"/delete", "", map[string]string{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("delete without session = %d", rec.Code)
	}

	for _, id := range []string{firstID, secondID} {
		if rec, _ := h.do(http.MethodPost, "/api/keys/"+id+"/revoke", session, map[string]string{}); rec.Code != http.StatusOK {
			t.Fatalf("revoke = %d", rec.Code)
		}
	}
	rec, _ = h.do(http.MethodPost, "/api/keys/"+firstID+"/delete", session, map[string]string{})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete revoked = %d %s", rec.Code, rec.Body.String())
	}
	keys, _ := h.srv.Keys.List(context.Background())
	if len(keys) != 2 {
		t.Fatalf("keys after delete = %d", len(keys))
	}

	rec, out := h.do(http.MethodPost, "/api/keys/purge", session, map[string]string{})
	if rec.Code != http.StatusOK || out["deleted"] != float64(1) {
		t.Fatalf("purge = %d %v", rec.Code, out)
	}
	keys, _ = h.srv.Keys.List(context.Background())
	if len(keys) != 1 || keys[0].ID != activeID {
		t.Fatalf("keys after purge = %+v", keys)
	}
	rec, out = h.do(http.MethodPost, "/api/keys/purge", session, map[string]string{})
	if rec.Code != http.StatusOK || out["deleted"] != float64(0) {
		t.Fatalf("second purge = %d %v", rec.Code, out)
	}
}

func TestCreateKeyRequiresName(t *testing.T) {
	h := newHarness(t)
	session := h.setup("correct horse")
	for _, name := range []string{"", "   ", "\t"} {
		rec, out := h.do(http.MethodPost, "/api/keys", session, map[string]string{"name": name})
		if rec.Code != http.StatusBadRequest || out["error"] != "키 이름을 입력해 주세요." {
			t.Fatalf("name %q = %d %v", name, rec.Code, out)
		}
	}
	rec, _ := h.do(http.MethodPost, "/api/keys", session, map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name = %d", rec.Code)
	}
	long := strings.Repeat("가", 65)
	if rec, _ := h.do(http.MethodPost, "/api/keys", session, map[string]string{"name": long}); rec.Code != http.StatusBadRequest {
		t.Fatalf("65 chars = %d", rec.Code)
	}
	rec, out := h.do(http.MethodPost, "/api/keys", session, map[string]string{"name": "  " + strings.Repeat("가", 64) + " "})
	if rec.Code != http.StatusOK || out["name"] != strings.Repeat("가", 64) {
		t.Fatalf("64 Korean chars = %d %v", rec.Code, out["name"])
	}
	keys, _ := h.srv.Keys.List(context.Background())
	if len(keys) != 1 {
		t.Fatalf("keys created = %d", len(keys))
	}
}
