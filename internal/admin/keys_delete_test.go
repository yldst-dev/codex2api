package admin

import (
	"context"
	"net/http"
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
