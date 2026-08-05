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
	t.Setenv("GHAPP_CACHE_DIR", "")
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
	t.Setenv("GHAPP_INSTALLATION_ID", "")
	t.Setenv("GHAPP_OWNER", "beta")
	t.Setenv("GHAPP_REPOSITORY", "")
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

func TestParseRejectsSecurityConfigurationOverrideInsideSession(t *testing.T) {
	cfg := Config{
		AppID:               "123",
		Host:                "github.com",
		APIURL:              "https://api.github.com",
		APIVersion:          "2026-03-10",
		NoCache:             true,
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
	t.Setenv(SessionConfigEnv, encoded)
	for _, args := range [][]string{
		{"--host", "attacker.example", "gh", "api", "/user"},
		{"--api-url", "https://attacker.example/api", "gh", "api", "/user"},
		{"--app-id", "999", "gh", "api", "/user"},
		{"--private-key", "/tmp/other.pem", "gh", "api", "/user"},
		{"--no-cache=false", "gh", "api", "/user"},
		{"--git-name", "Other", "gh", "api", "/user"},
		{"--git-email", "other@example.com", "gh", "api", "/user"},
		{"--git-authorship", "configured", "gh", "api", "/user"},
		{"--override-git-identity=false", "gh", "api", "/user"},
	} {
		if _, _, err := Parse(args); err == nil || !strings.Contains(err.Error(), "active ghapp credential session") {
			t.Fatalf("Parse(%v) error = %v", args, err)
		}
	}
}

func TestParseAllowsSelectionOverrideInsideSession(t *testing.T) {
	cfg := Config{
		Host:          "github.com",
		APIURL:        "https://api.github.com",
		APIVersion:    "2026-03-10",
		NoCache:       true,
		RefreshBefore: time.Minute,
		HTTPTimeout:   time.Second,
	}
	encoded, err := EncodeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionConfigEnv, encoded)
	loaded, rest, err := Parse([]string{"--owner", "beta", "--repository", "beta/two", "--installation-id", "7", "gh", "pr", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Owner != "beta" || loaded.Repository != "beta/two" || loaded.InstallationID != 7 {
		t.Fatalf("selection overrides were not retained: %+v", loaded)
	}
	if got := strings.Join(rest, " "); got != "gh pr list" {
		t.Fatalf("remaining args = %q", got)
	}
}

func TestGitAuthorshipDefaultsToBotWithEmptyConfiguredIdentity(t *testing.T) {
	t.Setenv(SessionConfigEnv, "")
	t.Setenv("GHAPP_GIT_NAME", "")
	t.Setenv("GHAPP_GIT_EMAIL", "")
	t.Setenv("GHAPP_GIT_AUTHORSHIP", "")
	t.Setenv("GHAPP_OVERRIDE_GIT_IDENTITY", "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitAuthorship != GitAuthorshipBot || cfg.GitName != "" || cfg.GitEmail != "" || !cfg.OverrideGitIdentity {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestConfiguredAuthorshipFallsBackToBotWhenIdentityIncomplete(t *testing.T) {
	for _, cfg := range []Config{
		{GitAuthorship: GitAuthorshipConfigured, GitName: "Agent"},
		{GitAuthorship: GitAuthorshipConfigured, GitEmail: "agent@example.com"},
		{GitAuthorship: GitAuthorshipBoth, GitName: "Agent"},
		{GitAuthorship: GitAuthorshipBoth, GitEmail: "agent@example.com"},
	} {
		cfg.Host = "github.com"
		cfg.RefreshBefore = time.Minute
		cfg.HTTPTimeout = time.Second
		normalized, err := normalize(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if normalized.GitAuthorship != GitAuthorshipBot {
			t.Fatalf("incomplete identity did not fall back: %+v", normalized)
		}
	}
}

func TestGitAuthorshipConfigurationFromEnvironment(t *testing.T) {
	t.Setenv(SessionConfigEnv, "")
	t.Setenv("GHAPP_GIT_NAME", "Agent User")
	t.Setenv("GHAPP_GIT_EMAIL", "agent@example.com")
	t.Setenv("GHAPP_GIT_AUTHORSHIP", "both")
	t.Setenv("GHAPP_OVERRIDE_GIT_IDENTITY", "false")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitName != "Agent User" || cfg.GitEmail != "agent@example.com" || cfg.GitAuthorship != GitAuthorshipBoth || cfg.OverrideGitIdentity {
		t.Fatalf("unexpected Git identity config: %+v", cfg)
	}
}

func TestGitAuthorshipRejectsInvalidValues(t *testing.T) {
	for _, cfg := range []Config{
		{GitAuthorship: "someone"},
		{GitAuthorship: GitAuthorshipConfigured, GitName: "Bad\nName", GitEmail: "a@example.com"},
		{GitAuthorship: GitAuthorshipConfigured, GitName: "Bad <Name>", GitEmail: "a@example.com"},
		{GitAuthorship: GitAuthorshipConfigured, GitName: "Agent", GitEmail: "bad email@example.com"},
	} {
		cfg.Host = "github.com"
		cfg.RefreshBefore = time.Minute
		cfg.HTTPTimeout = time.Second
		if _, err := normalize(cfg); err == nil {
			t.Fatalf("accepted invalid Git identity config: %+v", cfg)
		}
	}
}
