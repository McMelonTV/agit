package githubapp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTokenCacheTreatsUnreadableEntryAsMiss(t *testing.T) {
	cache := TokenCache{Dir: t.TempDir()}
	path := cache.path("123", "https://api.github.com", 7)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get("123", "https://api.github.com", 7, time.Minute); err != nil || ok {
		t.Fatalf("Get returned ok=%v err=%v", ok, err)
	}
}

func TestTokenCacheRejectsSymlinkDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "cache")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	cache := TokenCache{Dir: link}
	if err := cache.Put("123", "https://api.github.com", 7, Token{Token: "secret", ExpiresAt: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("expected symlink cache directory to be rejected")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache wrote through symlink: %v", entries)
	}
}
