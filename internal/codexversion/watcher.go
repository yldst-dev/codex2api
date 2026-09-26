package codexversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"codex-gateway/internal/oauth"
)

const (
	MetaKey    = "codex_client_version"
	ReleaseURL = "https://api.github.com/repos/openai/codex/releases/latest"
	Interval   = 6 * time.Hour
	FirstCheck = 10 * time.Second
	maxBody    = 1 << 20
)

type MetaStore interface {
	Meta(ctx context.Context, key string) (string, error)
	SetMeta(ctx context.Context, key, value string) error
}

type Status struct {
	Version   string
	Builtin   string
	CheckedAt time.Time
	Error     string
}

type Watcher struct {
	Store MetaStore
	HTTP  *http.Client
	URL   string
	Log   *slog.Logger

	mu        sync.Mutex
	checkedAt time.Time
	lastErr   string
}

func Load(ctx context.Context, store MetaStore) error {
	saved, err := store.Meta(ctx, MetaKey)
	if err != nil {
		return err
	}
	oauth.SetClientVersion(saved)
	return nil
}

func (w *Watcher) Run(ctx context.Context) {
	timer := time.NewTimer(FirstCheck)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if _, err := w.Check(ctx); err != nil && ctx.Err() == nil {
			w.logger().Warn("codex version check failed", "err", err)
		}
		timer.Reset(Interval)
	}
}

func (w *Watcher) Check(ctx context.Context) (string, error) {
	latest, err := w.latest(ctx)
	w.mu.Lock()
	w.checkedAt = time.Now()
	if err != nil {
		w.lastErr = err.Error()
	} else {
		w.lastErr = ""
	}
	w.mu.Unlock()
	if err != nil {
		return "", err
	}
	before := oauth.ClientVersion()
	if oauth.SetClientVersion(latest) {
		if err := w.Store.SetMeta(ctx, MetaKey, latest); err != nil {
			return latest, fmt.Errorf("save codex version: %w", err)
		}
		w.logger().Info("codex client version updated", "from", before, "to", latest)
	}
	return latest, nil
}

func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Status{
		Version:   oauth.ClientVersion(),
		Builtin:   oauth.Version,
		CheckedAt: w.checkedAt,
		Error:     w.lastErr,
	}
}

func (w *Watcher) latest(ctx context.Context) (string, error) {
	url := w.URL
	if url == "" {
		url = ReleaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "codex-gateway-version-check")
	client := w.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("check codex release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check codex release: status %d", resp.StatusCode)
	}
	var payload struct {
		Name       string `json:"name"`
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&payload); err != nil {
		return "", fmt.Errorf("check codex release: %w", err)
	}
	return VersionFromRelease(payload.Name, payload.TagName, payload.Draft || payload.Prerelease)
}

func VersionFromRelease(name, tag string, prerelease bool) (string, error) {
	if prerelease {
		return "", errors.New("latest codex release is a prerelease")
	}
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "rust-v")
	for _, candidate := range []string{strings.TrimSpace(name), tag} {
		if oauth.StableVersion(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("latest codex release has no stable X.Y.Z version (name %q, tag %q)", name, tag)
}

func (w *Watcher) logger() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}
