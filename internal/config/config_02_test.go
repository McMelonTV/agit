package config

import (
	"strings"
	"testing"
	"time"
)

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
		if _, _, err := Parse(args); err == nil || !strings.Contains(err.Error(), "active viagh credential session") {
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
