package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.2.0", "dev", false},
		{"v0.2.0-rc1", "v0.1.0", false},
		{"0.2.0", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Fatalf("Newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

type fakeRelease struct {
	tag    string
	binary []byte
	sums   string
}

func serve(t *testing.T, rel fakeRelease) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.invalid/r","published_at":"2026-09-26T00:00:00Z"}`, rel.tag)
	})
	mux.HandleFunc("GET /"+Repo+"/releases/download/"+rel.tag+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rel.sums))
	})
	mux.HandleFunc("GET /"+Repo+"/releases/download/"+rel.tag+"/"+AssetName(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(rel.binary)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := NewClient()
	c.APIBase, c.DownloadBase, c.AllowHTTP = srv.URL, srv.URL, true
	return c
}

func sumLine(body []byte, name string) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:]) + "  " + name + "\n"
}

func TestLatestAndInstall(t *testing.T) {
	binary := []byte("#!/bin/sh\necho new\n")
	c := serve(t, fakeRelease{
		tag:    "v0.2.0",
		binary: binary,
		sums:   sumLine([]byte("other"), "codex-gateway-plan9-mips") + sumLine(binary, AssetName()),
	})
	rel, err := c.Latest(context.Background())
	if err != nil || rel.Tag != "v0.2.0" {
		t.Fatalf("latest = %+v %v", rel, err)
	}
	exe := filepath.Join(t.TempDir(), "codex-gateway")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(context.Background(), rel.Tag, exe); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	info, _ := os.Stat(exe)
	if string(got) != string(binary) || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed %q mode %v", got, info.Mode())
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".codex-gateway-update-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
}

func TestInstallRejectsBadChecksumAndTag(t *testing.T) {
	c := serve(t, fakeRelease{
		tag:    "v0.2.0",
		binary: []byte("tampered"),
		sums:   sumLine([]byte("original"), AssetName()),
	})
	exe := filepath.Join(t.TempDir(), "codex-gateway")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := c.Install(context.Background(), "v0.2.0", exe)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("binary replaced after mismatch: %q", got)
	}
	for _, tag := range []string{"../../evil", "v1", "latest", "v0.2.0/../x"} {
		if err := c.Install(context.Background(), tag, exe); err == nil {
			t.Fatalf("accepted tag %q", tag)
		}
	}
}

func TestInstallRequiresChecksumEntry(t *testing.T) {
	c := serve(t, fakeRelease{tag: "v0.2.0", binary: []byte("x"), sums: sumLine([]byte("x"), "codex-gateway-plan9-mips")})
	exe := filepath.Join(t.TempDir(), "codex-gateway")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	if err := c.Install(context.Background(), "v0.2.0", exe); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestLatestRejectsOddTags(t *testing.T) {
	c := serve(t, fakeRelease{tag: "nightly"})
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("accepted a non-semver tag")
	}
}
