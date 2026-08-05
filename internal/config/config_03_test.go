package config

import (
	"testing"
	"time"
)

func TestGitAuthorshipDefaultsToBotWithEmptyConfiguredIdentity(t *testing.T) {
	t.Setenv(SessionConfigEnv, "")
	t.Setenv("VIAGH_GIT_NAME", "")
	t.Setenv("VIAGH_GIT_EMAIL", "")
	t.Setenv("VIAGH_GIT_AUTHORSHIP", "")
	t.Setenv("VIAGH_OVERRIDE_GIT_IDENTITY", "")
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
	t.Setenv("VIAGH_GIT_NAME", "Agent User")
	t.Setenv("VIAGH_GIT_EMAIL", "agent@example.com")
	t.Setenv("VIAGH_GIT_AUTHORSHIP", "both")
	t.Setenv("VIAGH_OVERRIDE_GIT_IDENTITY", "false")
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
