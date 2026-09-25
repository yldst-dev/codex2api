package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	Repo            = "yldst-dev/codex2api"
	DefaultAPIBase  = "https://api.github.com"
	DefaultDownload = "https://github.com"
	maxAPIBody      = 1 << 20
	maxSumsBody     = 64 << 10
	maxBinary       = 200 << 20
)

var tagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

var ErrNoRelease = errors.New("no published release")

type Release struct {
	Tag         string    `json:"tag"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

type Client struct {
	HTTP         *http.Client
	APIBase      string
	DownloadBase string
	AllowHTTP    bool
}

func NewClient() *Client {
	c := &Client{APIBase: DefaultAPIBase, DownloadBase: DefaultDownload}
	c.HTTP = &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && !c.AllowHTTP {
				return errors.New("refusing a non-https redirect")
			}
			return nil
		},
	}
	return c
}

func AssetName() string {
	return "codex-gateway-" + runtime.GOOS + "-" + runtime.GOARCH
}

func ValidTag(tag string) bool {
	return tagPattern.MatchString(tag)
}

func Newer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	c, ok := parse(current)
	if !ok {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(tag string) ([3]int, bool) {
	var out [3]int
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return out, false
	}
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func (c *Client) Latest(ctx context.Context) (Release, error) {
	url := strings.TrimRight(c.APIBase, "/") + "/repos/" + Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("check latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("check latest release: status %d", resp.StatusCode)
	}
	var raw struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBody)).Decode(&raw); err != nil {
		return Release{}, fmt.Errorf("check latest release: %w", err)
	}
	if raw.Draft || raw.Prerelease || !ValidTag(raw.TagName) {
		return Release{}, fmt.Errorf("latest release has an unexpected tag %q", raw.TagName)
	}
	return Release{Tag: raw.TagName, URL: raw.HTMLURL, PublishedAt: raw.PublishedAt}, nil
}

func (c *Client) Install(ctx context.Context, tag, exe string) error {
	if !ValidTag(tag) {
		return fmt.Errorf("invalid release tag %q", tag)
	}
	asset := AssetName()
	base := strings.TrimRight(c.DownloadBase, "/") + "/" + Repo + "/releases/download/" + tag + "/"
	want, err := c.checksum(ctx, base+"SHA256SUMS", asset)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".codex-gateway-update-*")
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	got, err := c.download(ctx, base+asset, tmp)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	if err := tmp.Chmod(0o755); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return fmt.Errorf("replace %s: %w", exe, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: status %d", filepath.Base(url), resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) checksum(ctx context.Context, url, asset string) ([]byte, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxSumsBody))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != asset {
			continue
		}
		sum, err := hex.DecodeString(fields[0])
		if err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("SHA256SUMS has a malformed line for %s", asset)
		}
		return sum, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("SHA256SUMS has no entry for %s", asset)
}

func (c *Client) download(ctx context.Context, url string, dst io.Writer) ([]byte, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(resp.Body, maxBinary+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", filepath.Base(url), err)
	}
	if n > maxBinary {
		return nil, fmt.Errorf("download %s: file is too large", filepath.Base(url))
	}
	return h.Sum(nil), nil
}

func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".codex-gateway-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
