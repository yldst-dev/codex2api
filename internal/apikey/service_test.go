package apikey

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"codex-gateway/internal/store"
)

func TestAPIKeyLifecycleAndLogs(t *testing.T) {
	st := openStore(t)
	var logs bytes.Buffer
	svc := NewService(st, bytes.Repeat([]byte{9}, 32), slog.New(slog.NewTextHandler(&logs, nil)))
	created, err := svc.Create(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Key, "cg_") || len(created.Key) < 40 {
		t.Fatalf("key = %s", created.Key)
	}
	if strings.Contains(created.Key[len(created.Prefix):], created.Prefix[3:]) && created.Prefix == created.Key {
		t.Fatal("prefix must be a short identifier")
	}
	key, err := svc.Authenticate(context.Background(), created.Key)
	if err != nil || key.ID != created.ID || key.Hash != nil {
		t.Fatalf("auth = %+v err %v", key, err)
	}
	if _, err := svc.Authenticate(context.Background(), created.Key+"x"); err != ErrInvalid {
		t.Fatalf("bad key err = %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), "nope"); err != ErrInvalid {
		t.Fatalf("bad key err = %v", err)
	}
	listed, err := svc.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Hash != nil || listed[0].LastUsedAt == nil {
		t.Fatalf("list = %+v err %v", listed, err)
	}
	if strings.Contains(logs.String(), created.Key) {
		t.Fatalf("log exposed api key: %s", logs.String())
	}
	rotated, err := svc.Rotate(context.Background(), created.ID)
	if err != nil || rotated.Key == created.Key {
		t.Fatalf("rotate = %+v err %v", rotated, err)
	}
	if _, err := svc.Authenticate(context.Background(), created.Key); err != ErrInvalid {
		t.Fatal("old key still worked")
	}
	if _, err := svc.Authenticate(context.Background(), rotated.Key); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), rotated.Key); err != ErrInvalid {
		t.Fatal("revoked key worked")
	}
	if _, err := svc.Rotate(context.Background(), created.ID); err == nil {
		t.Fatal("revoked key rotated")
	}
	if strings.Contains(logs.String(), created.Key) || strings.Contains(logs.String(), rotated.Key) {
		t.Fatalf("log exposed api key: %s", logs.String())
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir(), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
