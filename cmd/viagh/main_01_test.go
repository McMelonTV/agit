package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"

	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/githubapp"
	"github.com/McMelonTV/viagh/internal/wrapper"
)

func TestUnauthenticatedGHBypassesMalformedAppConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	marker := filepath.Join(t.TempDir(), "calls")
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ -n "$VIAGH_PRIVATE_KEY" ] || [ -n "$VIAGH_PRIVATE_KEY_PEM" ] || [ -n "$VIAGH_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
  echo "private key leaked" >&2
  exit 81
fi
printf 'called\n' >> "$CALL_MARKER"
printf 'gh version test\n'
`)
	t.Setenv("CALL_MARKER", marker)
	t.Setenv("VIAGH_PRIVATE_KEY_BASE64", "not-valid-base64")

	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "--real-gh", fakeGH, "gh", "--version"})
	})
	if code != 0 || stderr != "" || stdout != "gh version test\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	contents, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(contents), "called") != 1 {
		t.Fatalf("underlying gh call count: %q", contents)
	}
}

func TestRepoListHelpDoesNotFanOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	marker := filepath.Join(t.TempDir(), "calls")
	fakeGH := writeExecutable(t, `#!/bin/sh
printf 'called\n' >> "$CALL_MARKER"
printf 'repo list help\n'
`)
	t.Setenv("CALL_MARKER", marker)
	t.Setenv("VIAGH_PRIVATE_KEY_BASE64", "not-valid-base64")

	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "--real-gh=" + fakeGH, "gh", "repo", "list", "--help"})
	})
	if code != 0 || stderr != "" || stdout != "repo list help\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	contents, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(contents), "called") != 1 {
		t.Fatalf("underlying gh call count: %q", contents)
	}
}

func TestExplicitDifferentGHHostDoesNotReceiveAppSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$GH_TOKEN" != "user-token" ]; then
  echo "user token was changed: $GH_TOKEN" >&2
  exit 82
fi
if [ -n "$VIAGH_BROKER_URL" ] || [ -n "$VIAGH_BROKER_SECRET" ] || [ -n "$VIAGH_SESSION_CONFIG" ]; then
  echo "app broker leaked" >&2
  exit 83
fi
printf 'external-host-ok\n'
`)
	t.Setenv("GH_TOKEN", "user-token")
	t.Setenv("VIAGH_BROKER_URL", "http://127.0.0.1:1")
	t.Setenv("VIAGH_BROKER_SECRET", "secret")
	t.Setenv("VIAGH_SESSION_CONFIG", "session")
	cfg := config.Config{Host: "github.com", RealGH: fakeGH}
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"api", "--hostname", "github.example.com", "/user"})
	})
	if code != 0 || stderr != "" || stdout != "external-host-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRepositoryEnvironmentForDifferentHostDoesNotReceiveAppToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ -n "$GH_TOKEN" ] || [ -n "$GITHUB_TOKEN" ] || [ -n "$GH_ENTERPRISE_TOKEN" ] || [ -n "$GITHUB_ENTERPRISE_TOKEN" ]; then
  echo "app token leaked: $GH_TOKEN" >&2
  exit 85
fi
if [ -n "$VIAGH_ACTIVE_TOKEN_SHA256" ] || [ -n "$VIAGH_BROKER_URL" ] || [ -n "$VIAGH_BROKER_SECRET" ] || [ -n "$VIAGH_SESSION_CONFIG" ]; then
  echo "app session leaked" >&2
  exit 86
fi
if [ "$GH_REPO" != "github.other.example/acme/widgets" ] || [ "$GH_HOST" != "github.other.example" ]; then
  echo "repository routing lost: GH_REPO=$GH_REPO GH_HOST=$GH_HOST" >&2
  exit 87
fi
printf 'external-repository-ok\n'
`)
	base := wrapper.ApplyGHAuth(os.Environ(), "github.com", true, "app-token")
	for _, entry := range base {
		name, value, found := strings.Cut(entry, "=")
		if found {
			t.Setenv(name, value)
		}
	}
	cfg := config.Config{Host: "github.com", Repository: "github.other.example/acme/widgets", RealGH: fakeGH, SessionRestricted: true}
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"pr", "list"})
	})
	if code != 0 || stderr != "" || stdout != "external-repository-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestExplicitRepositoryOverridesAmbientDifferentHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 1, Account: githubapp.Account{Login: "acme"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/1/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-1", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$GH_TOKEN" != "token-1" ]; then
  echo "wrong token: $GH_TOKEN" >&2
  exit 91
fi
printf 'explicit-repository-ok\n'
`)
	cfg := testConfig(keyPath, server.URL)
	cfg.Repository = "github.other.example/wrong/repository"
	cfg.RealGH = fakeGH
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"pr", "list", "--repo", "acme/widgets"})
	})
	if code != 0 || stderr != "" || stdout != "explicit-repository-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
