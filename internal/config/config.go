package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultListen = "127.0.0.1:8080"
	EnvListen     = "CODEX_GATEWAY_LISTEN"
	EnvDataDir    = "CODEX_GATEWAY_DATA_DIR"
	EnvMasterKey  = "CODEX_GATEWAY_MASTER_KEY"
	MasterKeyFile = "master.key"
	MetaListen    = "listen"
)

type Config struct {
	Listen          string
	DataDir         string
	MasterKey       []byte
	MasterKeySource string
	GeneratedKey    bool
}

func Load() (*Config, error) {
	dataDir := strings.TrimSpace(os.Getenv(EnvDataDir))
	if dataDir == "" {
		dataDir = "data"
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("data dir permissions: %w", err)
	}

	cfg := &Config{
		Listen:  strings.TrimSpace(os.Getenv(EnvListen)),
		DataDir: abs,
	}
	if envKey := strings.TrimSpace(os.Getenv(EnvMasterKey)); envKey != "" {
		key, err := ParseMasterKey(envKey)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", EnvMasterKey, err)
		}
		cfg.MasterKey = key
		cfg.MasterKeySource = "env"
		return cfg, nil
	}

	path := filepath.Join(abs, MasterKeyFile)
	raw, err := os.ReadFile(path)
	if err == nil {
		key, err := ParseMasterKey(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("master key permissions: %w", err)
		}
		cfg.MasterKey = key
		cfg.MasterKeySource = "file"
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("master key: %w", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	cfg.MasterKey = key
	cfg.MasterKeySource = "file"
	cfg.GeneratedKey = true
	return cfg, nil
}

func ParseMasterKey(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("empty master key")
	}
	if len(s) == 64 && isHex(s) {
		key, err := hex.DecodeString(s)
		if err != nil {
			return nil, err
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("master key must be 32 bytes")
		}
		return key, nil
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		key, err := enc.DecodeString(s)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, fmt.Errorf("master key must be 32 bytes, hex or base64")
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func ResolveListen(envValue, stored string) string {
	if v := strings.TrimSpace(envValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(stored); v != "" {
		return v
	}
	return DefaultListen
}

func ValidateListen(addr string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("listen address must be host:port")
	}
	if host == "" || port == "" {
		return fmt.Errorf("listen address must be host:port")
	}
	return nil
}

func HealthURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", err
	}
	switch host {
	case "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/health", nil
}
