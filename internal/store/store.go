package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	OAuthActive  = "active"
	OAuthReauth  = "reauth_required"
	KeyActive    = "active"
	KeyRevoked   = "revoked"
	DBFile       = "gateway.db"
	metaKeyCheck = "master_key_check"
)

var ErrRevoked = errors.New("api key is revoked")

var ErrMasterKeyMismatch = errors.New("master key does not match this data directory")

type OAuthAccount struct {
	AccessToken    string
	RefreshToken   string
	IDToken        string
	ExpiresAt      time.Time
	AccountID      string
	UserID         string
	Email          string
	PlanType       string
	OrganizationID string
	ChatGPTUserID  string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type APIKey struct {
	ID         string
	Name       string
	Hash       []byte
	Prefix     string
	Status     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

type Store struct {
	db   *sql.DB
	path string
	key  []byte
}

func Open(dataDir string, masterKey []byte) (*Store, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, DBFile)
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: path, key: append([]byte(nil), masterKey...)}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.verifyKey(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.tighten(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_account (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  access_token BLOB NOT NULL,
  refresh_token BLOB NOT NULL,
  id_token BLOB,
  expires_at INTEGER NOT NULL,
  account_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  plan_type TEXT NOT NULL DEFAULT '',
  organization_id TEXT NOT NULL DEFAULT '',
  chatgpt_user_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  key_hash BLOB NOT NULL UNIQUE,
  key_prefix TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  revoked_at INTEGER
);
`)
	return err
}

func (s *Store) verifyKey(ctx context.Context) error {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("codex-gateway master key check"))
	want := hex.EncodeToString(mac.Sum(nil))
	stored, err := s.Meta(ctx, metaKeyCheck)
	if err != nil {
		return err
	}
	if stored != "" {
		if !hmac.Equal([]byte(stored), []byte(want)) {
			return ErrMasterKeyMismatch
		}
		return nil
	}
	if _, err := s.LoadOAuth(ctx); err != nil {
		return ErrMasterKeyMismatch
	}
	return s.SetMeta(ctx, metaKeyCheck, want)
}

func (s *Store) tighten() error {
	for _, path := range []string{s.path, s.path + "-wal", s.path + "-shm"} {
		if _, err := os.Stat(path); err == nil {
			if err := os.Chmod(path, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`, key, value)
	return err
}

func (s *Store) LoadOAuth(ctx context.Context) (*OAuthAccount, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT access_token, refresh_token, id_token, expires_at, account_id, user_id, email,
       plan_type, organization_id, chatgpt_user_id, status, created_at, updated_at
FROM oauth_account WHERE id = 1
`)
	var access, refresh, idToken []byte
	var expires, created, updated int64
	acc := &OAuthAccount{}
	err := row.Scan(
		&access, &refresh, &idToken, &expires, &acc.AccountID, &acc.UserID, &acc.Email,
		&acc.PlanType, &acc.OrganizationID, &acc.ChatGPTUserID, &acc.Status, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	acc.AccessToken, err = s.open(access, "access_token")
	if err != nil {
		return nil, fmt.Errorf("decrypt access token: %w", err)
	}
	acc.RefreshToken, err = s.open(refresh, "refresh_token")
	if err != nil {
		return nil, fmt.Errorf("decrypt refresh token: %w", err)
	}
	if len(idToken) > 0 {
		acc.IDToken, err = s.open(idToken, "id_token")
		if err != nil {
			return nil, fmt.Errorf("decrypt id token: %w", err)
		}
	}
	acc.ExpiresAt = time.Unix(expires, 0)
	acc.CreatedAt = time.Unix(created, 0)
	acc.UpdatedAt = time.Unix(updated, 0)
	return acc, nil
}

func (s *Store) SaveOAuth(ctx context.Context, acc OAuthAccount) error {
	if acc.AccessToken == "" || acc.RefreshToken == "" {
		return fmt.Errorf("oauth tokens are required")
	}
	if acc.Status == "" {
		acc.Status = OAuthActive
	}
	access, err := s.seal([]byte(acc.AccessToken), "access_token")
	if err != nil {
		return err
	}
	refresh, err := s.seal([]byte(acc.RefreshToken), "refresh_token")
	if err != nil {
		return err
	}
	var idToken any
	if acc.IDToken != "" {
		sealed, err := s.seal([]byte(acc.IDToken), "id_token")
		if err != nil {
			return err
		}
		idToken = sealed
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var created int64
	err = tx.QueryRowContext(ctx, `SELECT created_at FROM oauth_account WHERE id = 1`).Scan(&created)
	if errors.Is(err, sql.ErrNoRows) {
		created = now
	} else if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO oauth_account (
  id, access_token, refresh_token, id_token, expires_at, account_id, user_id, email,
  plan_type, organization_id, chatgpt_user_id, status, created_at, updated_at
) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  access_token = excluded.access_token,
  refresh_token = excluded.refresh_token,
  id_token = excluded.id_token,
  expires_at = excluded.expires_at,
  account_id = excluded.account_id,
  user_id = excluded.user_id,
  email = excluded.email,
  plan_type = excluded.plan_type,
  organization_id = excluded.organization_id,
  chatgpt_user_id = excluded.chatgpt_user_id,
  status = excluded.status,
  updated_at = excluded.updated_at
`, access, refresh, idToken, acc.ExpiresAt.Unix(), acc.AccountID, acc.UserID, acc.Email,
		acc.PlanType, acc.OrganizationID, acc.ChatGPTUserID, acc.Status, created, now)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.tighten()
}

func (s *Store) MarkReauth(ctx context.Context) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE oauth_account SET status = ?, updated_at = ? WHERE id = 1
`, OAuthReauth, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteOAuth(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_account WHERE id = 1`)
	return err
}

func (s *Store) InsertAPIKey(ctx context.Context, key APIKey) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO api_keys (id, name, key_hash, key_prefix, status, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, key.ID, key.Name, key.Hash, key.Prefix, KeyActive, key.CreatedAt.Unix())
	return err
}

func (s *Store) APIKeyByHash(ctx context.Context, hash []byte) (APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, key_hash, key_prefix, status, created_at, last_used_at, revoked_at
FROM api_keys WHERE key_hash = ?
`, hash)
	return scanKey(row)
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, key_hash, key_prefix, status, created_at, last_used_at, revoked_at
FROM api_keys
ORDER BY created_at DESC, id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		key, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanKey(row scanner) (APIKey, error) {
	var key APIKey
	var created int64
	var last, revoked sql.NullInt64
	if err := row.Scan(&key.ID, &key.Name, &key.Hash, &key.Prefix, &key.Status, &created, &last, &revoked); err != nil {
		return APIKey{}, err
	}
	key.CreatedAt = time.Unix(created, 0)
	if last.Valid {
		t := time.Unix(last.Int64, 0)
		key.LastUsedAt = &t
	}
	if revoked.Valid {
		t := time.Unix(revoked.Int64, 0)
		key.RevokedAt = &t
	}
	return key, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, id string) error {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
UPDATE api_keys
SET status = ?, revoked_at = COALESCE(revoked_at, ?)
WHERE id = ?
`, KeyRevoked, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) RotateAPIKey(ctx context.Context, id string, hash []byte, prefix string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id = ?`, id).Scan(&status)
	if err != nil {
		return err
	}
	if status != KeyActive {
		return ErrRevoked
	}
	_, err = tx.ExecContext(ctx, `
UPDATE api_keys SET key_hash = ?, key_prefix = ? WHERE id = ?
`, hash, prefix, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

func (s *Store) seal(plaintext []byte, aad string) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, []byte(aad)), nil
}

func (s *Store) open(sealed []byte, aad string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
