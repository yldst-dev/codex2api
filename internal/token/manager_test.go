package token

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codex-gateway/internal/oauth"
	"codex-gateway/internal/store"
)

func TestRefreshRotationSingleflightAndInvalidGrant(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-new",
				"refresh_token": "refresh-new",
				"expires_in":    3600,
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	st := openStore(t)
	mgr := NewManager(st, oauth.NewClient(srv.URL))
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(-time.Minute),
		AccountID:    "acc",
		Email:        "user@example.com",
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := mgr.AccessToken(context.Background())
			errs <- err
		}()
	}
	time.Sleep(50 * time.Millisecond)
	once.Do(func() { close(release) })
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d", calls.Load())
	}
	acc, err := st.LoadOAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acc.RefreshToken != "refresh-new" || acc.AccessToken != "access-new" || acc.Email != "user@example.com" {
		t.Fatalf("account = %+v", acc)
	}
}

func TestRefreshKeepsOldTokenWhenOmittedAndPreservesOnInvalidGrant(t *testing.T) {
	mode := "omit"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "omit" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-2",
				"expires_in":   3600,
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	st := openStore(t)
	mgr := NewManager(st, oauth.NewClient(srv.URL))
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-keep",
		ExpiresAt:    time.Now().Add(-time.Minute),
		AccountID:    "acc",
	}); err != nil {
		t.Fatal(err)
	}
	acc, err := mgr.ForceRefresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acc.RefreshToken != "refresh-keep" || acc.AccessToken != "access-2" {
		t.Fatalf("rotation omit = %+v", acc)
	}

	mode = "invalid"
	acc.ExpiresAt = time.Now().Add(-time.Minute)
	if err := st.SaveOAuth(context.Background(), *acc); err != nil {
		t.Fatal(err)
	}
	_, err = mgr.ForceRefresh(context.Background())
	if err == nil || err.Error() != ReauthMessage {
		t.Fatalf("err = %v", err)
	}
	kept, err := st.LoadOAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if kept.RefreshToken != "refresh-keep" || kept.AccessToken != "access-2" || kept.Status != store.OAuthReauth {
		t.Fatalf("tokens changed on invalid_grant: %+v", kept)
	}
}

func TestExpiredTokenRefreshesAndFreshTokenDoesNot(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-new",
			"refresh_token": "refresh-new",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	st := openStore(t)
	mgr := NewManager(st, oauth.NewClient(srv.URL))
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "access-fresh",
		RefreshToken: "refresh-fresh",
		ExpiresAt:    time.Now().Add(time.Hour),
		AccountID:    "acc",
	}); err != nil {
		t.Fatal(err)
	}
	got, _, err := mgr.AccessToken(context.Background())
	if err != nil || got != "access-fresh" || calls.Load() != 0 {
		t.Fatalf("got %s err %v calls %d", got, err, calls.Load())
	}

	acc, _ := st.LoadOAuth(context.Background())
	acc.ExpiresAt = time.Now().Add(time.Minute)
	if err := st.SaveOAuth(context.Background(), *acc); err != nil {
		t.Fatal(err)
	}
	got, _, err = mgr.AccessToken(context.Background())
	if err != nil || got != "access-new" || calls.Load() != 1 {
		t.Fatalf("near expiry got %s err %v calls %d", got, err, calls.Load())
	}
}

func TestTemporaryFailureKeepsUsableToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer srv.Close()
	st := openStore(t)
	mgr := NewManager(st, oauth.NewClient(srv.URL))
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "access-still-good",
		RefreshToken: "refresh-still-good",
		ExpiresAt:    time.Now().Add(time.Minute),
		AccountID:    "acc",
	}); err != nil {
		t.Fatal(err)
	}
	got, _, err := mgr.AccessToken(context.Background())
	if err != nil || got != "access-still-good" {
		t.Fatalf("got %s err %v", got, err)
	}
	acc, _ := st.LoadOAuth(context.Background())
	acc.ExpiresAt = time.Now().Add(-time.Minute)
	if err := st.SaveOAuth(context.Background(), *acc); err != nil {
		t.Fatal(err)
	}
	_, _, err = mgr.AccessToken(context.Background())
	var temporary *TemporaryError
	if err == nil || !asTemporary(err, &temporary) {
		t.Fatalf("err = %v", err)
	}
	kept, _ := st.LoadOAuth(context.Background())
	if kept.RefreshToken != "refresh-still-good" || kept.Status == store.OAuthReauth {
		t.Fatalf("temporary failure mutated account: %+v", kept)
	}
}

func asTemporary(err error, target **TemporaryError) bool {
	temporary, ok := err.(*TemporaryError)
	if !ok {
		return false
	}
	*target = temporary
	return true
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir(), []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestRotatedTokenIsSavedWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-rotated",
			"refresh_token": "refresh-rotated",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	st := openStore(t)
	mgr := NewManager(st, oauth.NewClient(srv.URL))
	if _, err := mgr.SaveLogin(context.Background(), &oauth.Token{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(-time.Minute),
		AccountID:    "acc",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr.AccessToken(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := st.LoadOAuth(context.Background())
	if err != nil || saved.RefreshToken != "refresh-rotated" {
		t.Fatalf("saved = %+v err %v", saved, err)
	}
}
