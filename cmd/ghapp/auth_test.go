package main

import (
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/agit/internal/broker"
	"github.com/McMelonTV/agit/internal/config"
	"github.com/McMelonTV/agit/internal/wrapper"
)

func TestBeginAuthSessionDoesNotTrustBrokerWithoutRestrictedSession(t *testing.T) {
	t.Setenv(broker.URLEnv, "http://127.0.0.1:1")
	t.Setenv(broker.SecretEnv, "injected")
	cfg := config.Config{Host: "github.com", APIURL: "https://api.github.com", NoCache: true, RefreshBefore: time.Minute, HTTPTimeout: time.Second}
	session, err := beginAuthSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.server == nil || session.client.URL == "http://127.0.0.1:1" {
		t.Fatalf("untrusted broker was reused: %+v", session.client)
	}
}

func TestBeginAuthSessionRequiresBrokerForRestrictedSession(t *testing.T) {
	t.Setenv(broker.URLEnv, "")
	t.Setenv(broker.SecretEnv, "")
	cfg := config.Config{Host: "github.com", APIURL: "https://api.github.com", NoCache: true, RefreshBefore: time.Minute, HTTPTimeout: time.Second, SessionRestricted: true}
	if _, err := beginAuthSession(cfg); err == nil || !strings.Contains(err.Error(), "broker is unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthSessionEnvironmentOmitsPrivateKeyAndNormalizesSelectors(t *testing.T) {
	cfg := config.Config{
		PrivateKeyPath: "/secret/key.pem",
		PrivateKeyPEM:  "secret-key",
		InstallationID: 7,
		Owner:          "acme",
		Repository:     "acme/widgets",
		Host:           "github.com",
		APIURL:         "https://api.github.com",
		NoCache:        true,
		RefreshBefore:  time.Minute,
		HTTPTimeout:    time.Second,
	}
	session, err := beginAuthSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	env := session.Environment([]string{
		"GHAPP_PRIVATE_KEY=/old/key",
		"GHAPP_PRIVATE_KEY_PEM=old-secret",
		"GHAPP_OWNER=wrong",
		"GH_REPO=wrong/repo",
	})
	for _, key := range []string{"GHAPP_PRIVATE_KEY", "GHAPP_PRIVATE_KEY_PEM", "GHAPP_PRIVATE_KEY_BASE64", "GITHUB_APP_PRIVATE_KEY"} {
		if _, ok := wrapper.LookupEnv(env, key); ok {
			t.Fatalf("%s leaked", key)
		}
	}
	for key, want := range map[string]string{
		"GHAPP_INSTALLATION_ID": "7",
		"GHAPP_OWNER":           "acme",
		"GHAPP_REPOSITORY":      "acme/widgets",
		"GH_REPO":               "acme/widgets",
		"GH_HOST":               "github.com",
	} {
		if got, ok := wrapper.LookupEnv(env, key); !ok || got != want {
			t.Fatalf("%s = %q, %v; want %q", key, got, ok, want)
		}
	}
	encoded, _ := wrapper.LookupEnv(env, config.SessionConfigEnv)
	if strings.Contains(encoded, "secret") {
		t.Fatal("encoded session contains private key material")
	}
}

func TestRestrictedAuthSessionRemovesInheritedAppTokenBeforeSanitizingMarker(t *testing.T) {
	cfg := config.Config{
		Host:              "github.com",
		APIURL:            "https://api.github.com",
		NoCache:           true,
		RefreshBefore:     time.Minute,
		HTTPTimeout:       time.Second,
		SessionRestricted: true,
	}
	encoded, err := config.EncodeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session := &authSession{cfg: cfg, client: broker.Client{URL: "http://127.0.0.1:1", Secret: "secret"}, encoded: encoded}

	base := wrapper.ApplyGHAuth(nil, "github.com", true, "app-token")
	base = wrapper.SetEnv(base, "GH_TOKEN", "user-token")
	env := session.Environment(base)
	if value, ok := wrapper.LookupEnv(env, "GH_TOKEN"); !ok || value != "user-token" {
		t.Fatalf("user override was not preserved: %q, %v", value, ok)
	}
	if _, ok := wrapper.LookupEnv(env, "GITHUB_TOKEN"); ok {
		t.Fatal("inherited App fallback token was retained")
	}
	if _, ok := wrapper.LookupEnv(env, "GHAPP_ACTIVE_TOKEN_SHA256"); ok {
		t.Fatal("active token marker was retained")
	}
}

func TestTopLevelAuthSessionIgnoresUntrustedGitTrackingMarker(t *testing.T) {
	cfg := config.Config{Host: "github.com", APIURL: "https://api.github.com", NoCache: true, RefreshBefore: time.Minute, HTTPTimeout: time.Second}
	encoded, err := config.EncodeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session := &authSession{cfg: cfg, client: broker.Client{URL: "http://127.0.0.1:1", Secret: "secret"}, encoded: encoded}
	env := session.Environment([]string{
		"GHAPP_GIT_CONFIG_START=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.autocrlf",
		"GIT_CONFIG_VALUE_0=false",
	})
	if _, ok := wrapper.LookupEnv(env, "GHAPP_GIT_CONFIG_START"); ok {
		t.Fatal("untrusted tracking marker was retained")
	}
	if value, _ := wrapper.LookupEnv(env, "GIT_CONFIG_KEY_0"); value != "core.autocrlf" {
		t.Fatalf("user Git config was altered: %q", value)
	}
}
