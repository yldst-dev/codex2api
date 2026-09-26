package codexversion

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"codex-gateway/internal/oauth"
)

type memMeta map[string]string

func (m memMeta) Meta(_ context.Context, key string) (string, error) { return m[key], nil }

func (m memMeta) SetMeta(_ context.Context, key, value string) error {
	m[key] = value
	return nil
}

func releaseServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newer(t *testing.T) string {
	t.Helper()
	var major, minor, patch int
	if _, err := fmt.Sscanf(oauth.Version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

func TestCheckAppliesAndSavesNewerStableVersion(t *testing.T) {
	t.Cleanup(oauth.ResetClientVersion)
	next := newer(t)
	store := memMeta{}
	w := &Watcher{Store: store, URL: releaseServer(t, fmt.Sprintf(`{"name":"%s","tag_name":"rust-v%s"}`, next, next))}
	got, err := w.Check(context.Background())
	if err != nil || got != next {
		t.Fatalf("check = %q %v", got, err)
	}
	if oauth.ClientVersion() != next || oauth.ClientUserAgent() != "codex-tui/"+next+" (Ubuntu 22.4.0; x86_64) xterm-256color" {
		t.Fatalf("runtime version = %s / %s", oauth.ClientVersion(), oauth.ClientUserAgent())
	}
	if store[MetaKey] != next {
		t.Fatalf("saved = %q", store[MetaKey])
	}
	if st := w.Status(); st.Version != next || st.Builtin != oauth.Version || st.CheckedAt.IsZero() || st.Error != "" {
		t.Fatalf("status = %+v", st)
	}

	oauth.ResetClientVersion()
	if err := Load(context.Background(), store); err != nil || oauth.ClientVersion() != next {
		t.Fatalf("load after restart = %s %v", oauth.ClientVersion(), err)
	}
}

func TestCheckNeverDowngradesOrAcceptsPrereleases(t *testing.T) {
	t.Cleanup(oauth.ResetClientVersion)
	for _, body := range []string{
		`{"name":"0.1.0","tag_name":"rust-v0.1.0"}`,
		`{"name":"999.0.0-alpha.1","tag_name":"rust-v999.0.0-alpha.1"}`,
		`{"name":"999.0.0","tag_name":"rust-v999.0.0","prerelease":true}`,
		`{"name":"nightly","tag_name":"latest"}`,
	} {
		store := memMeta{}
		w := &Watcher{Store: store, URL: releaseServer(t, body)}
		_, _ = w.Check(context.Background())
		if oauth.ClientVersion() != oauth.Version || store[MetaKey] != "" {
			t.Fatalf("%s changed version to %s (saved %q)", body, oauth.ClientVersion(), store[MetaKey])
		}
	}
	if err := Load(context.Background(), memMeta{MetaKey: "0.0.1"}); err != nil || oauth.ClientVersion() != oauth.Version {
		t.Fatalf("an old saved version lowered the built-in one: %s", oauth.ClientVersion())
	}
}

func TestCheckKeepsVersionWhenGitHubFails(t *testing.T) {
	t.Cleanup(oauth.ResetClientVersion)
	next := newer(t)
	oauth.SetClientVersion(next)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	w := &Watcher{Store: memMeta{}, URL: srv.URL}
	if _, err := w.Check(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if oauth.ClientVersion() != next || w.Status().Error == "" {
		t.Fatalf("version = %s, status = %+v", oauth.ClientVersion(), w.Status())
	}
}

func TestVersionFromRelease(t *testing.T) {
	cases := []struct {
		name, tag string
		pre       bool
		want      string
	}{
		{"0.157.1", "rust-v0.157.1", false, "0.157.1"},
		{"Codex 0.158.0", "rust-v0.158.0", false, "0.158.0"},
		{"", "rust-v0.158.2", false, "0.158.2"},
		{"0.158.0", "rust-v0.158.0", true, ""},
		{"", "rust-v0.158.0-alpha.2", false, ""},
	}
	for _, c := range cases {
		got, err := VersionFromRelease(c.name, c.tag, c.pre)
		if got != c.want || (c.want == "" && err == nil) {
			t.Fatalf("VersionFromRelease(%q, %q, %v) = %q %v", c.name, c.tag, c.pre, got, err)
		}
	}
}
