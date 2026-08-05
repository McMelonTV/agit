package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

func TestMalformedBase64PrivateKeyIsLazy(t *testing.T) {
	t.Setenv(config.SessionConfigEnv, "")
	t.Setenv("VIAGH_APP_ID", "123")
	t.Setenv("VIAGH_PRIVATE_KEY_BASE64", "not-valid-base64")
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("configuration was decoded eagerly: %v", err)
	}
	backend := &localBackend{cfg: cfg}
	if _, err := backend.appClient(); err == nil || !strings.Contains(err.Error(), "decode VIAGH_PRIVATE_KEY_BASE64") {
		t.Fatalf("unexpected lazy key error: %v", err)
	}
}

func TestLocalGitCommandDoesNotDecodeOrExposePrivateKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	fakeGit := writeExecutable(t, `#!/bin/sh
if [ -n "$VIAGH_PRIVATE_KEY" ] || [ -n "$VIAGH_PRIVATE_KEY_PEM" ] || [ -n "$VIAGH_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
  echo "private key leaked" >&2
  exit 84
fi
printf 'git version test\n'
`)
	t.Setenv(config.SessionConfigEnv, "")
	t.Setenv("VIAGH_APP_ID", "123")
	t.Setenv("VIAGH_PRIVATE_KEY_BASE64", "not-valid-base64")
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "--real-git", fakeGit, "git", "--version"})
	})
	if code != 0 || stderr != "" || stdout != "git version test\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestAmbientRepositoryOnDifferentHostKeepsUserAuthentication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	gitPath := realGit(t)
	dir := t.TempDir()
	runGitCommand(t, gitPath, dir, "init")
	runGitCommand(t, gitPath, dir, "remote", "add", "origin", "https://github.other.example/acme/widgets.git")
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$GH_TOKEN" != "user-token" ]; then
  echo "user token changed: $GH_TOKEN" >&2
  exit 97
fi
if [ -n "$VIAGH_BROKER_URL" ] || [ -n "$VIAGH_BROKER_SECRET" ] || [ -n "$VIAGH_SESSION_CONFIG" ]; then
  echo "App session leaked" >&2
  exit 98
fi
if [ "$GH_REPO:$GH_HOST" != "github.other.example/acme/widgets:github.other.example" ]; then
  echo "ambient repository routing lost: $GH_REPO/$GH_HOST" >&2
  exit 99
fi
printf 'ambient-external-host-ok\n'
`)
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	t.Setenv("GH_TOKEN", "user-token")
	t.Setenv("VIAGH_BROKER_URL", "")
	t.Setenv("VIAGH_BROKER_SECRET", "")
	t.Setenv("VIAGH_SESSION_CONFIG", "")

	cfg := config.Config{Host: "github.com", RealGH: fakeGH, RealGit: gitPath}
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"pr", "list"})
	})
	if code != 0 || stderr != "" || stdout != "ambient-external-host-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func runGitCommand(t *testing.T, gitPath, dir string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command(gitPath, commandArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
