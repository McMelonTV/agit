package githubapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TokenCache struct {
	Dir      string
	Disabled bool
	Now      func() time.Time
}

func (c TokenCache) Get(appID, apiURL string, installationID int64, minValidity time.Duration) (Token, bool, error) {
	if c.Disabled || c.Dir == "" {
		return Token{}, false, nil
	}
	if err := c.ensureDir(); err != nil {
		return Token{}, false, nil
	}
	path := c.path(appID, apiURL, installationID)
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Token{}, false, nil
	}
	if err != nil {
		_ = os.Remove(path)
		return Token{}, false, nil
	}
	var token Token
	if err := json.Unmarshal(contents, &token); err != nil {
		_ = os.Remove(path)
		return Token{}, false, nil
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	if token.Token == "" || !token.ExpiresAt.After(now.Add(minValidity)) {
		_ = os.Remove(path)
		return Token{}, false, nil
	}
	return token, true, nil
}

func (c TokenCache) Put(appID, apiURL string, installationID int64, token Token) error {
	if c.Disabled || c.Dir == "" {
		return nil
	}
	if err := c.ensureDir(); err != nil {
		return err
	}
	contents, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encode token cache: %w", err)
	}
	path := c.path(appID, apiURL, installationID)
	tmp, err := os.CreateTemp(c.Dir, ".token-*")
	if err != nil {
		return fmt.Errorf("create token cache file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure token cache file: %w", err)
	}
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		return fmt.Errorf("write token cache: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync token cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close token cache: %w", err)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit token cache: %w", err)
	}
	return nil
}

func (c TokenCache) Lock(ctx context.Context, appID, apiURL string, installationID int64) (func(), bool) {
	if c.Disabled || c.Dir == "" {
		return func() {}, false
	}
	if err := c.ensureDir(); err != nil {
		return func() {}, false
	}
	lockPath := c.path(appID, apiURL, installationID) + ".lock"
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, true
		}
		if !errors.Is(err, os.ErrExist) {
			return func() {}, false
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 2*time.Minute {
			_ = os.Remove(lockPath)
			continue
		}
		select {
		case <-ctx.Done():
			return func() {}, false
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (c TokenCache) ensureDir() error {
	info, err := os.Lstat(c.Dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(c.Dir, 0o700); err != nil {
			return fmt.Errorf("create token cache directory: %w", err)
		}
		info, err = os.Lstat(c.Dir)
	}
	if err != nil {
		return fmt.Errorf("inspect token cache directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("token cache directory must not be a symbolic link")
	}
	if !info.IsDir() {
		return errors.New("token cache path is not a directory")
	}
	if err := os.Chmod(c.Dir, 0o700); err != nil {
		return fmt.Errorf("secure token cache directory: %w", err)
	}
	return nil
}

func (c TokenCache) path(appID, apiURL string, installationID int64) string {
	identity := strings.Join([]string{appID, apiURL, fmt.Sprint(installationID)}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return filepath.Join(c.Dir, hex.EncodeToString(sum[:])[:32]+".json")
}
