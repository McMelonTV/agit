package config

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDefaultAPIURL(t *testing.T) {
	tests := map[string]string{
		"github.com":         "https://api.github.com",
		"tenant.ghe.com":     "https://api.tenant.ghe.com",
		"github.example.com": "https://github.example.com/api/v3",
	}
	for host, want := range tests {
		if got := defaultAPIURL(host); got != want {
			t.Fatalf("defaultAPIURL(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestNormalizeRejectsNonHTTPAPIURL(t *testing.T) {
	for _, value := range []string{
		"/api/v3",
		"file:///tmp/api",
		"https://user:pass@example.com",
		"https://example.com/api?x=1",
		"http://github.example.com/api/v3",
	} {
		_, err := normalize(Config{Host: "github.com", APIURL: value, RefreshBefore: time.Minute, HTTPTimeout: time.Second})
		if err == nil {
			t.Fatalf("accepted invalid API URL %q", value)
		}
	}
}

func TestNormalizeAllowsLoopbackHTTPAPIURL(t *testing.T) {
	for _, value := range []string{
		"http://localhost:8080/api/v3",
		"http://127.0.0.1:8080/api/v3",
		"http://[::1]:8080/api/v3",
	} {
		if _, err := normalize(Config{Host: "github.com", APIURL: value, RefreshBefore: time.Minute, HTTPTimeout: time.Second}); err != nil {
			t.Fatalf("rejected loopback API URL %q: %v", value, err)
		}
	}
}

func TestSessionConfigurationOmitsPrivateKey(t *testing.T) {
	cfg := Config{
		AppID:               "123",
		PrivateKeyPath:      "/secret/key.pem",
		PrivateKeyPEM:       "PRIVATE KEY MATERIAL",
		Owner:               "acme",
		Host:                "github.com",
		APIURL:              "https://api.github.com",
		APIVersion:          "2026-03-10",
		CacheDir:            "/tmp/cache",
		RefreshBefore:       time.Minute,
		HTTPTimeout:         time.Second,
		GitName:             "Agent",
		GitEmail:            "agent@example.com",
		GitAuthorship:       GitAuthorshipBoth,
		OverrideGitIdentity: true,
	}
	encoded, err := EncodeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(decoded), "secret") || strings.Contains(string(decoded), "PRIVATE KEY") {
		t.Fatalf("session configuration contains private-key material: %s", decoded)
	}
	var raw map[string]any
	if err := json.Unmarshal(decoded, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["owner"] != "acme" {
		t.Fatalf("owner was not retained: %#v", raw)
	}

	t.Setenv(SessionConfigEnv, encoded)
	loaded, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Owner != "acme" || !loaded.SessionRestricted || loaded.GitName != "Agent" || loaded.GitEmail != "agent@example.com" || loaded.GitAuthorship != GitAuthorshipBoth || !loaded.OverrideGitIdentity {
		t.Fatalf("unexpected loaded session config: %+v", loaded)
	}
	if loaded.PrivateKeyPath != "" || loaded.PrivateKeyPEM != "" {
		t.Fatal("private key was restored into session config")
	}
}

func TestCacheDisabledWhenUserCacheDirectoryUnavailable(t *testing.T) {
	t.Setenv(SessionConfigEnv, "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("VIAGH_CACHE_DIR", "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NoCache || cfg.CacheDir != "" {
		t.Fatalf("cache fallback is not disabled: %+v", cfg)
	}
}

func TestNormalizeRejectsInvalidHost(t *testing.T) {
	for _, value := range []string{"github.com/path", "user@github.com", "github.com?x=1", "github.com#fragment"} {
		_, err := normalize(Config{Host: value, RefreshBefore: time.Minute, HTTPTimeout: time.Second})
		if err == nil {
			t.Fatalf("accepted invalid host %q", value)
		}
	}
}

func TestSessionSelectionCanBeOverriddenByNestedEnvironment(t *testing.T) {
	cfg := Config{
		InstallationID: 7,
		Owner:          "alpha",
		Repository:     "alpha/one",
		Host:           "github.com",
		APIURL:         "https://api.github.com",
		APIVersion:     "2026-03-10",
		NoCache:        true,
		RefreshBefore:  time.Minute,
		HTTPTimeout:    time.Second,
	}
	encoded, err := EncodeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionConfigEnv, encoded)
	t.Setenv("VIAGH_INSTALLATION_ID", "")
	t.Setenv("VIAGH_OWNER", "beta")
	t.Setenv("VIAGH_REPOSITORY", "")
	t.Setenv("GH_REPO", "beta/two")
	loaded, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.InstallationID != 0 || loaded.Owner != "beta" || loaded.Repository != "" {
		t.Fatalf("unexpected overrides: %+v", loaded)
	}
}

func TestNormalizeRemovesDefaultHTTPSPort(t *testing.T) {
	cfg, err := normalize(Config{Host: "github.com:443", RefreshBefore: time.Minute, HTTPTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "github.com" || cfg.APIURL != "https://api.github.com" || !cfg.CloudHost() {
		t.Fatalf("unexpected normalized config: %+v", cfg)
	}
}

func TestSessionGHRepoOverride(t *testing.T) {
	cfg := Config{Host: "github.com", APIURL: "https://api.github.com", NoCache: true, RefreshBefore: time.Minute, HTTPTimeout: time.Second}
	encoded, err := EncodeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionConfigEnv, encoded)
	t.Setenv("GH_REPO", "beta/two")
	loaded, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Repository != "beta/two" {
		t.Fatalf("repository = %q", loaded.Repository)
	}
}
