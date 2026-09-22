package apikey

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"codex-gateway/internal/store"
)

const Prefix = "cg_"

var ErrInvalid = errors.New("invalid api key")

type Service struct {
	store  *store.Store
	master []byte
	log    *slog.Logger
}

func NewService(st *store.Store, master []byte, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, master: append([]byte(nil), master...), log: log}
}

type Created struct {
	ID     string
	Name   string
	Prefix string
	Key    string
}

func (s *Service) Create(ctx context.Context, name string) (Created, error) {
	name, err := cleanName(name)
	if err != nil {
		return Created{}, err
	}
	id, full, prefix, sum, err := material(s.master)
	if err != nil {
		return Created{}, err
	}
	err = s.store.InsertAPIKey(ctx, store.APIKey{
		ID:        id,
		Name:      name,
		Hash:      sum,
		Prefix:    prefix,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return Created{}, err
	}
	return Created{ID: id, Name: name, Prefix: prefix, Key: full}, nil
}

func (s *Service) List(ctx context.Context) ([]store.APIKey, error) {
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		keys[i].Hash = nil
	}
	return keys, nil
}

func (s *Service) Revoke(ctx context.Context, id string) error {
	err := s.store.RevokeAPIKey(ctx, strings.TrimSpace(id))
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("api key not found")
	}
	return err
}

func (s *Service) Rotate(ctx context.Context, id string) (Created, error) {
	id = strings.TrimSpace(id)
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		return Created{}, err
	}
	var name string
	found := false
	for _, key := range keys {
		if key.ID == id {
			found = true
			name = key.Name
			break
		}
	}
	if !found {
		return Created{}, fmt.Errorf("api key not found")
	}
	_, full, prefix, sum, err := material(s.master)
	if err != nil {
		return Created{}, err
	}
	if err := s.store.RotateAPIKey(ctx, id, sum, prefix); err != nil {
		if errors.Is(err, store.ErrRevoked) {
			return Created{}, fmt.Errorf("api key is revoked")
		}
		if errors.Is(err, sql.ErrNoRows) {
			return Created{}, fmt.Errorf("api key not found")
		}
		return Created{}, err
	}
	return Created{ID: id, Name: name, Prefix: prefix, Key: full}, nil
}

func (s *Service) Authenticate(ctx context.Context, presented string) (store.APIKey, error) {
	presented = strings.TrimSpace(presented)
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		s.log.Warn("api key lookup failed")
		return store.APIKey{}, ErrInvalid
	}
	sum := Hash(s.master, []byte(presented))
	found := -1
	for i := range keys {
		if subtle.ConstantTimeCompare(keys[i].Hash, sum) == 1 && found < 0 {
			found = i
		}
	}
	if found < 0 || keys[found].Status != store.KeyActive {
		s.log.Info("api key rejected")
		return store.APIKey{}, ErrInvalid
	}
	if err := s.store.TouchAPIKey(ctx, keys[found].ID); err != nil {
		s.log.Warn("api key last-used update failed", "key_id", keys[found].ID)
	}
	keys[found].Hash = nil
	return keys[found], nil
}

func Hash(master, key []byte) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write(key)
	return mac.Sum(nil)
}

func material(master []byte) (id, full, prefix string, sum []byte, err error) {
	rawID := make([]byte, 8)
	if _, err = rand.Read(rawID); err != nil {
		return "", "", "", nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return "", "", "", nil, err
	}
	full = Prefix + base64.RawURLEncoding.EncodeToString(secret)
	prefix = full
	if len(prefix) > 11 {
		prefix = prefix[:11]
	}
	return hex.EncodeToString(rawID), full, prefix, Hash(master, []byte(full)), nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("api key name is required")
	}
	if len(name) > 64 {
		return "", fmt.Errorf("api key name is too long")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("api key name contains a control character")
		}
	}
	return name, nil
}
