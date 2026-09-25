package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestOAuthRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	key := []byte("0123456789abcdef0123456789abcdef")
	st, err := Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	info, err := os.Stat(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("db mode = %o", info.Mode().Perm())
	}
	acc := OAuthAccount{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		IDToken:      "id-secret",
		ExpiresAt:    time.Unix(1_800_000_000, 0),
		AccountID:    "acc",
		Email:        "user@example.com",
		PlanType:     "plus",
		Status:       OAuthActive,
	}
	if err := st.SaveOAuth(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadOAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != acc.AccessToken || loaded.RefreshToken != acc.RefreshToken || loaded.IDToken != acc.IDToken || loaded.Email != acc.Email {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, []byte("abcdef0123456789abcdef0123456789")); !errors.Is(err, ErrMasterKeyMismatch) {
		t.Fatalf("open with wrong master key err = %v", err)
	}
}

func TestLegacyStoreWithoutKeyCheckRejectsWrongKey(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveOAuth(context.Background(), OAuthAccount{AccessToken: "a", RefreshToken: "b", ExpiresAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DELETE FROM meta WHERE key = ?`, metaKeyCheck); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if _, err := Open(dir, []byte("abcdef0123456789abcdef0123456789")); !errors.Is(err, ErrMasterKeyMismatch) {
		t.Fatalf("err = %v", err)
	}
	again, err := Open(dir, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	var timeout int
	if err := again.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 5000 {
		t.Fatalf("busy_timeout = %d, %v", timeout, err)
	}
}

func TestCreatedAtPreserved(t *testing.T) {
	st := openStore(t)
	acc := OAuthAccount{AccessToken: "a", RefreshToken: "b", ExpiresAt: time.Now().Add(time.Hour), Status: OAuthActive}
	if err := st.SaveOAuth(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	first, err := st.LoadOAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	first.AccessToken = "c"
	if err := st.SaveOAuth(context.Background(), *first); err != nil {
		t.Fatal(err)
	}
	second, err := st.LoadOAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("created %s -> %s updated %s -> %s", first.CreatedAt, second.CreatedAt, first.UpdatedAt, second.UpdatedAt)
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir(), []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
