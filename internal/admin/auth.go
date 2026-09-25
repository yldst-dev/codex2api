package admin

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"codex-gateway/internal/config"
	"codex-gateway/internal/store"
)

const (
	MetaPassword  = "admin_password"
	minPassword   = 8
	maxPassword   = 1024
	passwordAlgo  = "pbkdf2-sha256"
	setupTokenLen = 32
)

var passwordIterations = 600_000

func PasswordSet(ctx context.Context, st *store.Store) (bool, error) {
	stored, err := st.Meta(ctx, MetaPassword)
	return stored != "", err
}

func EnsureSetupToken(ctx context.Context, st *store.Store, dataDir string) (string, error) {
	set, err := PasswordSet(ctx, st)
	if err != nil || set {
		return "", err
	}
	if token, err := readSetupToken(dataDir); err == nil {
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	token, err := randomHex(setupTokenLen)
	if err != nil {
		return "", err
	}
	path := setupTokenPath(dataDir)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write setup token: %w", err)
	}
	if err := matchDirOwner(path, dataDir); err != nil {
		return "", err
	}
	return token, nil
}

func matchDirOwner(path, dir string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return os.Chown(path, int(st.Uid), int(st.Gid))
}

func ResetPassword(ctx context.Context, st *store.Store, dataDir string) (string, error) {
	if err := st.SetMeta(ctx, MetaPassword, ""); err != nil {
		return "", err
	}
	if err := os.Remove(setupTokenPath(dataDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return EnsureSetupToken(ctx, st, dataDir)
}

func SetupURL(adminListen, token string) string {
	host, port, err := net.SplitHostPort(adminListen)
	if err == nil && (host == "" || host == "0.0.0.0" || host == "::") {
		adminListen = net.JoinHostPort("127.0.0.1", port)
	}
	return "http://" + adminListen + "/#setup=" + token
}

func setupTokenPath(dataDir string) string {
	return filepath.Join(dataDir, config.SetupTokenFile)
}

func readSetupToken(dataDir string) (string, error) {
	raw, err := os.ReadFile(setupTokenPath(dataDir))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if len(token) != setupTokenLen*2 {
		return "", fmt.Errorf("setup token file is malformed")
	}
	return token, nil
}

func checkSetupToken(dataDir, presented string) bool {
	token, err := readSetupToken(dataDir)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(strings.TrimSpace(presented))) == 1
}

func validatePassword(pw string) error {
	switch {
	case len([]rune(pw)) < minPassword:
		return fmt.Errorf("비밀번호는 %d자 이상이어야 합니다", minPassword)
	case len(pw) > maxPassword:
		return fmt.Errorf("비밀번호가 너무 깁니다")
	}
	return nil
}

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum, err := pbkdf2.Key(sha256.New, pw, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return strings.Join([]string{passwordAlgo, strconv.Itoa(passwordIterations), enc.EncodeToString(salt), enc.EncodeToString(sum)}, "$"), nil
}

func checkPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != passwordAlgo {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
