package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestMenuKeys(t *testing.T) {
	if classifyMenuKey("\x1b[A") != menuUp || classifyMenuKey("\x1bOB") != menuDown {
		t.Fatal("arrows")
	}
	if classifyMenuKey("\r") != menuEnter || classifyMenuKey("q") != menuQuit || classifyMenuKey("\x03") != menuQuit {
		t.Fatal("enter and quit")
	}
	if classifyMenuKey("x") != menuNone {
		t.Fatal("other")
	}
}

func TestNoArgsWithoutTerminalPrintsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code %d %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "arrow-key menu") {
		t.Fatalf("help = %s", stdout.String())
	}
}

func TestKeyCommandsJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_GATEWAY_DATA_DIR", dir)
	t.Setenv("CODEX_GATEWAY_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32)))
	t.Setenv("CODEX_GATEWAY_LISTEN", "127.0.0.1:8080")

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"key", "create", "--name", "default", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code %d stderr %s", code, stderr.String())
	}
	var created struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Key    string `json:"key"`
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "default" || !strings.HasPrefix(created.Key, "cg_") {
		t.Fatalf("created = %+v", created)
	}

	stdout.Reset()
	if code := Run([]string{"key", "list", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("list code %d %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), created.Key) {
		t.Fatalf("list exposed key: %s", stdout.String())
	}

	stdout.Reset()
	if code := Run([]string{"--json", "key", "rotate", created.ID}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("rotate code %d %s", code, stderr.String())
	}
	var rotated struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.Key == "" || rotated.Key == created.Key {
		t.Fatalf("rotated = %+v", rotated)
	}

	stdout.Reset()
	if code := Run([]string{"key", "revoke", created.ID}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("revoke code %d %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), created.Key) || strings.Contains(stderr.String(), rotated.Key) {
		t.Fatalf("stderr leaked key: %s", stderr.String())
	}
}

func TestConfigAndAuthStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_GATEWAY_DATA_DIR", dir)
	t.Setenv("CODEX_GATEWAY_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)))
	t.Setenv("CODEX_GATEWAY_LISTEN", "")

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "set", "listen", "127.0.0.1:9090"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("set code %d %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"config", "show", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("show code %d %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "127.0.0.1:9090") || strings.Contains(stdout.String(), "MASTER") {
		t.Fatalf("show = %s", stdout.String())
	}
	stdout.Reset()
	if code := Run([]string{"auth", "status"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("status code %d %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Not logged in.") {
		t.Fatalf("status = %s", stdout.String())
	}
}
