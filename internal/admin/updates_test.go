package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codex-gateway/internal/update"
)

func fakeReleases(t *testing.T, tag string, binary []byte) *update.Client {
	t.Helper()
	sum := sha256.Sum256(binary)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/"+update.Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.invalid/r","published_at":"2026-09-26T00:00:00Z"}`, tag)
	})
	mux.HandleFunc("GET /"+update.Repo+"/releases/download/"+tag+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), update.AssetName())
	})
	mux.HandleFunc("GET /"+update.Repo+"/releases/download/"+tag+"/"+update.AssetName(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(binary)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := update.NewClient()
	c.APIBase, c.DownloadBase, c.AllowHTTP = srv.URL, srv.URL, true
	return c
}

func updateField(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	u, ok := out["update"].(map[string]any)
	if !ok {
		t.Fatalf("no update field in %v", out)
	}
	return u
}

func TestUpdateThroughSystemdHelper(t *testing.T) {
	h := newHarness(t)
	h.srv.Version = "v0.1.0"
	h.srv.Updates = fakeReleases(t, "v0.2.0", []byte("new"))
	h.srv.UpdateMode = UpdateModeSystemd
	session := h.setup("correct horse")

	_, out := h.do(http.MethodGet, "/api/state", session, nil)
	u := updateField(t, out)
	if u["current"] != "v0.1.0" || u["auto"] != true || u["supported"] != true || u["available"] != false {
		t.Fatalf("initial update state = %v", u)
	}
	rec, out := h.do(http.MethodPost, "/api/update/check", session, map[string]string{})
	u = updateField(t, out)
	if rec.Code != http.StatusOK || u["available"] != true {
		t.Fatalf("check = %d %v", rec.Code, u)
	}
	rec, out = h.do(http.MethodPost, "/api/update/apply", session, map[string]string{})
	if rec.Code != http.StatusAccepted || updateField(t, out)["status"] != statusRequested {
		t.Fatalf("apply = %d %v", rec.Code, out)
	}
	req, err := os.ReadFile(filepath.Join(h.dir, UpdateRequestFile))
	if err != nil || string(req) != "v0.2.0\n" {
		t.Fatalf("request file = %q %v", req, err)
	}
	info, _ := os.Stat(filepath.Join(h.dir, UpdateRequestFile))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("request mode = %v", info.Mode())
	}
	rec, _ = h.do(http.MethodPost, "/api/update/apply", session, map[string]string{})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second apply = %d", rec.Code)
	}

	h.srv.updMu.Lock()
	h.srv.upd.requestedAt = time.Now().Add(-requestStaleAfter - time.Second)
	h.srv.updMu.Unlock()
	_, out = h.do(http.MethodGet, "/api/state", session, nil)
	if updateField(t, out)["status"] != statusError {
		t.Fatalf("stale request was not reported: %v", out)
	}
	if _, err := os.Stat(filepath.Join(h.dir, UpdateRequestFile)); !os.IsNotExist(err) {
		t.Fatalf("stale request file kept: %v", err)
	}

	rec, out = h.do(http.MethodPost, "/api/update/auto", session, map[string]bool{"enabled": false})
	if rec.Code != http.StatusOK || updateField(t, out)["auto"] != false {
		t.Fatalf("auto off = %d %v", rec.Code, out)
	}
	if rec, _ = h.do(http.MethodPost, "/api/update/check", "", map[string]string{}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated check = %d", rec.Code)
	}
}

func TestSelfUpdateReplacesBinaryAndRestarts(t *testing.T) {
	h := newHarness(t)
	h.srv.Version = "v0.1.0"
	h.srv.Updates = fakeReleases(t, "v0.2.0", []byte("new binary"))
	h.srv.UpdateMode = UpdateModeSelf
	h.srv.Executable = filepath.Join(t.TempDir(), "codex-gateway")
	if err := os.WriteFile(h.srv.Executable, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	restarted := make(chan struct{})
	h.srv.Restart = func() { close(restarted) }
	session := h.setup("correct horse")

	h.do(http.MethodPost, "/api/update/check", session, map[string]string{})
	rec, _ := h.do(http.MethodPost, "/api/update/apply", session, map[string]string{})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply = %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("restart was not triggered")
	}
	if got, _ := os.ReadFile(h.srv.Executable); string(got) != "new binary" {
		t.Fatalf("binary = %q", got)
	}
}

func TestUpdateRefusedForDevAndUnsupportedInstalls(t *testing.T) {
	h := newHarness(t)
	h.srv.Updates = fakeReleases(t, "v0.2.0", []byte("x"))
	h.srv.Version = "dev"
	h.srv.UpdateMode = UpdateModeSelf
	session := h.setup("correct horse")
	h.do(http.MethodPost, "/api/update/check", session, map[string]string{})
	_, out := h.do(http.MethodGet, "/api/state", session, nil)
	if u := updateField(t, out); u["supported"] != false || u["available"] != false {
		t.Fatalf("dev build update state = %v", u)
	}
	if rec, _ := h.do(http.MethodPost, "/api/update/apply", session, map[string]string{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("dev apply = %d", rec.Code)
	}
	h.srv.Version = "v0.1.0"
	h.srv.UpdateMode = ""
	if rec, _ := h.do(http.MethodPost, "/api/update/apply", session, map[string]string{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported apply = %d", rec.Code)
	}
}
