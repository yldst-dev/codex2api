package token

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"codex-gateway/internal/oauth"
	"codex-gateway/internal/store"
)

const RefreshSkew = 3 * time.Minute

const ReauthMessage = "OAuth session is no longer valid.\nRun:\n\ncodex-gateway auth login"

var ErrReauth = errors.New(ReauthMessage)

type TemporaryError struct {
	Err error
}

func (e *TemporaryError) Error() string {
	if e.Err == nil {
		return "temporary token refresh error"
	}
	return "temporary token refresh error: " + e.Err.Error()
}

func (e *TemporaryError) Unwrap() error { return e.Err }

type Manager struct {
	store    *store.Store
	client   *oauth.Client
	lockPath string
	mu       sync.Mutex
	unsaved  *store.OAuthAccount
}

func NewManager(st *store.Store, client *oauth.Client) *Manager {
	if client == nil {
		client = oauth.NewClient("")
	}
	return &Manager{
		store:    st,
		client:   client,
		lockPath: filepath.Join(filepath.Dir(st.Path()), "refresh.lock"),
	}
}

func (m *Manager) Account(ctx context.Context) (*store.OAuthAccount, error) {
	return m.store.LoadOAuth(ctx)
}

func (m *Manager) SaveLogin(ctx context.Context, tok *oauth.Token) (*store.OAuthAccount, error) {
	if tok == nil || tok.AccessToken == "" || tok.RefreshToken == "" {
		return nil, fmt.Errorf("oauth response did not include a refresh token")
	}
	existing, err := m.store.LoadOAuth(ctx)
	if err != nil {
		return nil, err
	}
	acc := accountFromToken(tok, existing)
	acc.Status = store.OAuthActive
	if err := m.store.SaveOAuth(ctx, acc); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.unsaved = nil
	m.mu.Unlock()
	return m.store.LoadOAuth(ctx)
}

func (m *Manager) Logout(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unsaved = nil
	return m.store.DeleteOAuth(ctx)
}

func (m *Manager) AccessToken(ctx context.Context) (string, *store.OAuthAccount, error) {
	acc, err := m.current(ctx)
	if err != nil {
		return "", nil, err
	}
	if acc == nil {
		return "", nil, ErrReauth
	}
	if acc.Status == store.OAuthReauth {
		return "", acc, ErrReauth
	}
	if time.Until(acc.ExpiresAt) > RefreshSkew {
		return acc.AccessToken, acc, nil
	}
	acc, err = m.refresh(ctx, false)
	if err != nil {
		return "", nil, err
	}
	return acc.AccessToken, acc, nil
}

func (m *Manager) ForceRefresh(ctx context.Context) (*store.OAuthAccount, error) {
	return m.refresh(ctx, true)
}

func (m *Manager) current(ctx context.Context) (*store.OAuthAccount, error) {
	m.mu.Lock()
	unsaved := m.unsaved
	m.mu.Unlock()
	if unsaved != nil {
		if err := m.store.SaveOAuth(ctx, *unsaved); err == nil {
			m.mu.Lock()
			m.unsaved = nil
			m.mu.Unlock()
		}
		return cloneAccount(unsaved), nil
	}
	return m.store.LoadOAuth(ctx)
}

func (m *Manager) refresh(ctx context.Context, force bool) (*store.OAuthAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshLocked(ctx, force)
}

func (m *Manager) refreshLocked(ctx context.Context, force bool) (*store.OAuthAccount, error) {
	return withFileLock(m.lockPath, func() (*store.OAuthAccount, error) {
		var acc *store.OAuthAccount
		var err error
		if m.unsaved != nil {
			acc = cloneAccount(m.unsaved)
			if saveErr := m.store.SaveOAuth(ctx, *acc); saveErr == nil {
				m.unsaved = nil
			}
		} else {
			acc, err = m.store.LoadOAuth(ctx)
			if err != nil {
				return nil, err
			}
		}
		if acc == nil || acc.RefreshToken == "" {
			return nil, ErrReauth
		}
		if acc.Status == store.OAuthReauth && !force {
			return nil, ErrReauth
		}
		if !force && time.Until(acc.ExpiresAt) > RefreshSkew {
			return acc, nil
		}
		refreshCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := m.client.Refresh(refreshCtx, acc.RefreshToken)
		if err != nil {
			var endpoint *oauth.EndpointError
			if errors.As(err, &endpoint) && !endpoint.Temporary {
				_ = m.store.MarkReauth(ctx)
				if loaded, loadErr := m.store.LoadOAuth(ctx); loadErr == nil && loaded != nil {
					loaded.Status = store.OAuthReauth
					m.unsaved = nil
				}
				return nil, ErrReauth
			}
			if time.Until(acc.ExpiresAt) > 0 && acc.Status != store.OAuthReauth {
				return acc, nil
			}
			return nil, &TemporaryError{Err: err}
		}
		if tok.RefreshToken == "" {
			tok.RefreshToken = acc.RefreshToken
		}
		next := accountFromToken(tok, acc)
		next.Status = store.OAuthActive
		if err := m.store.SaveOAuth(ctx, next); err != nil {
			m.unsaved = cloneAccount(&next)
			for i := 0; i < 2; i++ {
				if retry := m.store.SaveOAuth(ctx, next); retry == nil {
					m.unsaved = nil
					break
				}
			}
			if m.unsaved != nil {
				return cloneAccount(m.unsaved), nil
			}
		}
		m.unsaved = nil
		saved, err := m.store.LoadOAuth(ctx)
		if err != nil || saved == nil {
			return &next, nil
		}
		return saved, nil
	})
}

func accountFromToken(tok *oauth.Token, prev *store.OAuthAccount) store.OAuthAccount {
	acc := store.OAuthAccount{
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		IDToken:        tok.IDToken,
		ExpiresAt:      tok.ExpiresAt,
		AccountID:      tok.AccountID,
		UserID:         tok.UserID,
		Email:          tok.Email,
		PlanType:       tok.PlanType,
		OrganizationID: tok.OrganizationID,
		ChatGPTUserID:  tok.ChatGPTUserID,
		Status:         store.OAuthActive,
	}
	if prev == nil {
		return acc
	}
	if acc.RefreshToken == "" {
		acc.RefreshToken = prev.RefreshToken
	}
	if acc.IDToken == "" {
		acc.IDToken = prev.IDToken
	}
	if acc.AccountID == "" {
		acc.AccountID = prev.AccountID
	}
	if acc.UserID == "" {
		acc.UserID = prev.UserID
	}
	if acc.Email == "" {
		acc.Email = prev.Email
	}
	if acc.PlanType == "" {
		acc.PlanType = prev.PlanType
	}
	if acc.OrganizationID == "" {
		acc.OrganizationID = prev.OrganizationID
	}
	if acc.ChatGPTUserID == "" {
		acc.ChatGPTUserID = prev.ChatGPTUserID
	}
	return acc
}

func cloneAccount(in *store.OAuthAccount) *store.OAuthAccount {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func withFileLock(path string, fn func() (*store.OAuthAccount, error)) (*store.OAuthAccount, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
