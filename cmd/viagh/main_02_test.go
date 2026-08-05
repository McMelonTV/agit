package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/githubapp"
	"github.com/McMelonTV/viagh/internal/wrapper"
)

func TestOwnerArgumentOverridesAmbientRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/beta/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 2, Account: githubapp.Account{Login: "beta"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/2/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-2", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$GH_TOKEN" != "token-2" ]; then
  echo "wrong token: $GH_TOKEN" >&2
  exit 92
fi
printf 'owner-ok\n'
`)
	cfg := testConfig(keyPath, server.URL)
	cfg.Repository = "alpha/one"
	cfg.RealGH = fakeGH
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"secret", "list", "--org", "beta"})
	})
	if code != 0 || stderr != "" || stdout != "owner-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRepoCreateUsesOwnerInstallationInsteadOfRepositoryLookup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/acme/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 3, Account: githubapp.Account{Login: "acme"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/3/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-3", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$GH_TOKEN" != "token-3" ]; then
  echo "wrong token: $GH_TOKEN" >&2
  exit 93
fi
printf 'create-ok\n'
`)
	cfg := testConfig(keyPath, server.URL)
	cfg.RealGH = fakeGH
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"repo", "create", "acme/widgets", "--private"})
	})
	if code != 0 || stderr != "" || stdout != "create-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRepoCreateQualifiesUnownedNameWithInstallationAccount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]githubapp.Installation{{ID: 4, Account: githubapp.Account{Login: "acme", Type: "Organization"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/4/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-4", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$1:$2:$3:$GH_TOKEN" != "repo:create:acme/widgets:token-4" ]; then
  echo "unexpected create invocation: $* token=$GH_TOKEN" >&2
  exit 94
fi
printf 'qualified-create-ok\n'
`)
	fakeGit := writeExecutable(t, `#!/bin/sh
if [ "$1:$2:$3" = "remote:get-url:origin" ]; then
  printf 'https://github.com/McMelonTV/viagh.git\n'
  exit 0
fi
exit 1
`)
	cfg := testConfig(keyPath, server.URL)
	cfg.RealGH = fakeGH
	cfg.RealGit = fakeGit
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"repo", "create", "widgets", "--private"})
	})
	if code != 0 || stderr != "" || stdout != "qualified-create-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestCommandGroupHelpBypassesMalformedConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	fakeGH := writeExecutable(t, "#!/bin/sh\nprintf 'repo help\\n'\n")
	t.Setenv("VIAGH_PRIVATE_KEY_BASE64", "not-valid-base64")
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "--real-gh", fakeGH, "gh", "repo"})
	})
	if code != 0 || stderr != "" || stdout != "repo help\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestTopLevelVersionBypassesConfiguration(t *testing.T) {
	t.Setenv("VIAGH_PRIVATE_KEY_BASE64", "not-valid-base64")
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "--version"})
	})
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "viagh ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestTopLevelHelpAndVersionWordsBypassInvalidConfiguration(t *testing.T) {
	t.Setenv("VIAGH_INSTALLATION_ID", "not-a-number")
	for _, command := range []string{"help", "--help", "-h", "version"} {
		stdout, stderr, code := captureProcessOutput(t, func() int {
			return run([]string{"viagh", command})
		})
		if code != 0 || stderr != "" || stdout == "" {
			t.Fatalf("command=%s code=%d stdout=%q stderr=%q", command, code, stdout, stderr)
		}
	}
}

func TestRestrictedUtilityCommandReportsMissingBroker(t *testing.T) {
	t.Setenv("VIAGH_BROKER_URL", "")
	t.Setenv("VIAGH_BROKER_SECRET", "")
	cfg := config.Config{Host: "github.com", APIURL: "https://api.github.com", HTTPTimeout: time.Second, SessionRestricted: true}
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runInstallations(cfg)
	})
	if code != 1 || stdout != "" || !strings.Contains(stderr, "credential broker is unavailable") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestTopLevelPassthroughDoesNotTrustGitConfigTrackingMarker(t *testing.T) {
	env := passthroughEnvironment([]string{
		"VIAGH_GIT_CONFIG_START=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=manager",
	})
	if value, _ := wrapper.LookupEnv(env, "GIT_CONFIG_COUNT"); value != "1" {
		t.Fatalf("user Git config count was changed: %q", value)
	}
	if value, _ := wrapper.LookupEnv(env, "GIT_CONFIG_KEY_0"); value != "credential.helper" {
		t.Fatalf("user Git config was stripped: %q", value)
	}
	if _, ok := wrapper.LookupEnv(env, "VIAGH_GIT_CONFIG_START"); ok {
		t.Fatal("untrusted tracking marker was retained")
	}
}
