package config

import (
	"os"
	"path/filepath"
	"testing"

	"codex-gateway/internal/store"
)

func TestLoadGeneratesKeyOnlyForEmptyDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvMasterKey, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.GeneratedKey || len(cfg.MasterKey) != 32 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if err := os.Remove(filepath.Join(dir, MasterKeyFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, store.DBFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("generated a new master key next to an existing database")
	}
	if _, err := os.Stat(filepath.Join(dir, MasterKeyFile)); !os.IsNotExist(err) {
		t.Fatalf("master key file was written: %v", err)
	}
}

func TestParseMasterKey(t *testing.T) {
	if _, err := ParseMasterKey("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseMasterKey("short"); err == nil {
		t.Fatal("accepted a short key")
	}
}
