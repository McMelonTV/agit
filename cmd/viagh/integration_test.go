package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/McMelonTV/viagh/internal/githubapp"
)

func TestExecRoutesNestedGHAcrossInstallationsWithoutPrivateKeyLeak(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell integration is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	var mu sync.Mutex
	var tokenIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/one/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 1, Account: githubapp.Account{Login: "acme"}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/beta/two/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 2, Account: githubapp.Account{Login: "beta"}})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/app/installations/") && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			id := strings.Split(r.URL.Path, "/")[3]
			mu.Lock()
			tokenIDs = append(tokenIDs, id)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-" + id, ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fakeGH := writeExecutable(t, `#!/bin/sh
if [ -n "$VIAGH_PRIVATE_KEY" ] || [ -n "$VIAGH_PRIVATE_KEY_PEM" ] || [ -n "$VIAGH_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
  echo "private key leaked" >&2
  exit 90
fi
case "$3:$GH_TOKEN" in
  acme/one:token-1) echo acme-ok ;;
  beta/two:token-2) echo beta-ok ;;
  *) echo "unexpected invocation: $* token=$GH_TOKEN" >&2; exit 91 ;;
esac
`)
	gitPath := realGit(t)
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--real-gh", fakeGH,
		"--real-git", gitPath,
		"--no-cache",
		"exec", "--", "/bin/sh", "-c", "gh repo view acme/one && gh repo view beta/two",
	)
	if code != 0 || stderr != "" || stdout != "acme-ok\nbeta-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(tokenIDs, ",") != "1,2" {
		t.Fatalf("token requests = %v", tokenIDs)
	}
}

func TestExecGitShimUsesCredentialBrokerWithCLIOnlyConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell integration is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/one/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 1, Account: githubapp.Account{Login: "acme"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/1/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "git-token", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	gitPath := realGit(t)
	input := "protocol=https\\nhost=github.com\\npath=acme/one.git\\n\\n"
	command := "printf '" + input + "' | git credential fill"
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--real-git", gitPath,
		"--no-cache",
		"exec", "--", "/bin/sh", "-c", command,
	)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "username=x-access-token\n") || !strings.Contains(stdout, "password=git-token\n") {
		t.Fatalf("credential output = %q", stdout)
	}
}

func runMainSubprocess(t *testing.T, stdin []byte, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(),
		"VIAGH_TEST_RUN_MAIN=1",
		"VIAGH_PRIVATE_KEY=",
		"VIAGH_PRIVATE_KEY_PEM=",
		"VIAGH_PRIVATE_KEY_BASE64=",
		"GITHUB_APP_PRIVATE_KEY=",
		"VIAGH_BROKER_URL=",
		"VIAGH_BROKER_SECRET=",
		"VIAGH_SESSION_CONFIG=",
		"GH_TOKEN=",
		"GITHUB_TOKEN=",
		"GH_ENTERPRISE_TOKEN=",
		"GITHUB_ENTERPRISE_TOKEN=",
		"VIAGH_GIT_NAME=",
		"VIAGH_GIT_EMAIL=",
		"VIAGH_GIT_AUTHORSHIP=",
		"VIAGH_OVERRIDE_GIT_IDENTITY=",
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err := cmd.Run()
	if err == nil {
		return stdoutBuffer.String(), stderrBuffer.String(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return stdoutBuffer.String(), stderrBuffer.String(), exitErr.ExitCode()
	}
	t.Fatalf("start subprocess: %v", err)
	return "", "", 1
}

func TestExecNestedGHRepoEnvironmentSelectsInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell integration is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/beta/two/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 2, Account: githubapp.Account{Login: "beta"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/2/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-2", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$GH_REPO:$GH_TOKEN" != "beta/two:token-2" ]; then
  echo "unexpected repository/token: $GH_REPO/$GH_TOKEN" >&2
  exit 92
fi
printf 'env-route-ok\n'
`)
	gitPath := realGit(t)
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--real-gh", fakeGH,
		"--real-git", gitPath,
		"--no-cache",
		"exec", "--", "/bin/sh", "-c", "GH_REPO=beta/two gh pr list",
	)
	if code != 0 || stderr != "" || stdout != "env-route-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestExecNestedDifferentGHHostBypassesAppBroker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell integration is Unix-specific")
	}
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$GH_HOST:$GH_TOKEN" != "github.other.example:user-token" ]; then
  echo "unexpected host/token: $GH_HOST/$GH_TOKEN" >&2
  exit 93
fi
if [ -n "$VIAGH_BROKER_URL" ] || [ -n "$VIAGH_BROKER_SECRET" ] || [ -n "$VIAGH_SESSION_CONFIG" ]; then
  echo "broker leaked to external host command" >&2
  exit 94
fi
printf 'external-env-host-ok\n'
`)
	gitPath := realGit(t)
	t.Setenv("GH_TOKEN", "user-token")
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--host", "github.com",
		"--real-gh", fakeGH,
		"--real-git", gitPath,
		"exec", "--", "/bin/sh", "-c", "GH_HOST=github.other.example GH_TOKEN=user-token gh api /user",
	)
	if code != 0 || stderr != "" || stdout != "external-env-host-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestDirectGHAllowsNestedGHToSwitchInstallations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell integration is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/one/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 1, Account: githubapp.Account{Login: "acme"}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/beta/two/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 2, Account: githubapp.Account{Login: "beta"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/1/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-1", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/2/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-2", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
case "$3:$GH_TOKEN" in
  acme/one:token-1) gh repo view beta/two ;;
  beta/two:token-2) printf 'nested-gh-ok\n' ;;
  *) echo "unexpected nested invocation: $* token=$GH_TOKEN" >&2; exit 95 ;;
esac
`)
	gitPath := realGit(t)
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--real-gh", fakeGH,
		"--real-git", gitPath,
		"--no-cache",
		"gh", "repo", "view", "acme/one",
	)
	if code != 0 || stderr != "" || stdout != "nested-gh-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestDirectGitProvidesShimsToHooksAndNestedCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell integration is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/beta/two/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 2, Account: githubapp.Account{Login: "beta"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/2/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-2", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
if [ -n "$VIAGH_PRIVATE_KEY" ] || [ -n "$VIAGH_PRIVATE_KEY_PEM" ] || [ -n "$VIAGH_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
  echo "private key leaked to nested gh" >&2
  exit 96
fi
if [ "$GH_TOKEN" != "token-2" ]; then
  echo "unexpected nested gh token: $GH_TOKEN" >&2
  exit 97
fi
printf 'git-nested-gh-ok\n'
`)
	fakeGit := writeExecutable(t, `#!/bin/sh
if [ "$1" = "config" ] && [ "$2" = "--get" ]; then
  exit 1
fi
if [ -n "$VIAGH_PRIVATE_KEY" ] || [ -n "$VIAGH_PRIVATE_KEY_PEM" ] || [ -n "$VIAGH_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
  echo "private key leaked to git" >&2
  exit 98
fi
gh pr list --repo beta/two
`)
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--real-gh", fakeGH,
		"--real-git", fakeGit,
		"--no-cache",
		"git", "status",
	)
	if code != 0 || stderr != "" || stdout != "git-nested-gh-ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestGitCredentialFailureDoesNotFallThroughToAskPass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell integration is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	gitPath := realGit(t)
	marker := t.TempDir() + "/askpass-called"
	askpass := writeExecutable(t, "#!/bin/sh\nprintf called >"+shellSingleQuote(marker)+"\nprintf fallback-secret\n")
	t.Setenv("GIT_ASKPASS", askpass)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	input := "protocol=https\\nhost=github.com\\npath=acme/missing.git\\n\\n"
	command := "printf '" + input + "' | git credential fill"
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--real-git", gitPath,
		"--no-cache",
		"exec", "--", "/bin/sh", "-c", command,
	)
	if code == 0 {
		t.Fatalf("expected credential fill failure; stdout=%q stderr=%q", stdout, stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("GIT_ASKPASS was invoked after helper failure: %v", err)
	}
	if !strings.Contains(stderr, "find installation for acme/missing") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestDefaultGitAuthorshipUsesAppBotAndOverridesClientConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git hook integration is covered separately on Windows CI")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)
	keyPath := writeTestPrivateKey(t)
	server := botIdentityServer(t, "test-app", 12345)
	defer server.Close()

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--real-git", gitPath,
		"--no-cache",
		"git", "-C", dir, "commit", "--allow-empty", "-m", "bot commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, message := commitIdentity(t, gitPath, dir)
	want := "test-app[bot]|12345+test-app[bot]@users.noreply.127.0.0.1|test-app[bot]|12345+test-app[bot]@users.noreply.127.0.0.1"
	if identity != want || strings.TrimSpace(message) != "bot commit" {
		t.Fatalf("identity=%q message=%q", identity, message)
	}
}

func TestConfiguredGitAuthorshipOverridesClientConfigWithoutAppLookup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git process integration is covered separately on Windows CI")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--git-name", "Configured Agent",
		"--git-email", "agent@example.com",
		"--git-authorship", "configured",
		"--real-git", gitPath,
		"git", "-C", dir,
		"-c", "user.name=Command Line User",
		"-c", "user.email=command@example.com",
		"commit", "--allow-empty", "-m", "configured commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, _ := commitIdentity(t, gitPath, dir)
	if identity != "Configured Agent|agent@example.com|Configured Agent|agent@example.com" {
		t.Fatalf("identity=%q", identity)
	}
}

func TestBothGitAuthorshipAddsBotCoauthorAndPreservesExistingHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hook integration is Unix-specific")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)
	hookDir := t.TempDir()
	marker := filepath.Join(hookDir, "prepare-hook-called")
	preCommitMarker := filepath.Join(hookDir, "pre-commit-called")
	runGitCommand(t, gitPath, dir, "config", "core.hooksPath", hookDir)
	hook := filepath.Join(hookDir, "prepare-commit-msg")
	contents := "#!/bin/sh\nprintf called >" + shellSingleQuote(marker) + "\nprintf '\\nHook-added-by-client\\n' >> \"$1\"\n"
	if err := os.WriteFile(hook, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	preCommitHook := filepath.Join(hookDir, "pre-commit")
	preCommitContents := "#!/bin/sh\nprintf called >" + shellSingleQuote(preCommitMarker) + "\n"
	if err := os.WriteFile(preCommitHook, []byte(preCommitContents), 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := writeTestPrivateKey(t)
	server := botIdentityServer(t, "test-app", 12345)
	defer server.Close()

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--git-name", "Configured Agent",
		"--git-email", "agent@example.com",
		"--git-authorship", "both",
		"--real-git", gitPath,
		"--no-cache",
		"git", "-C", dir, "commit", "--allow-empty", "-m", "combined commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, message := commitIdentity(t, gitPath, dir)
	if identity != "Configured Agent|agent@example.com|Configured Agent|agent@example.com" {
		t.Fatalf("identity=%q", identity)
	}
	trailer := "Co-authored-by: test-app[bot] <12345+test-app[bot]@users.noreply.127.0.0.1>"
	if strings.Count(message, trailer) != 1 {
		t.Fatalf("message=%q", message)
	}
	if hookIndex, trailerIndex := strings.Index(message, "Hook-added-by-client"), strings.Index(message, trailer); hookIndex < 0 || trailerIndex < 0 || hookIndex > trailerIndex {
		t.Fatalf("client hook must run before viagh appends the trailer: %q", message)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("existing prepare-commit-msg hook did not run: %v", err)
	}
	if _, err := os.Stat(preCommitMarker); err != nil {
		t.Fatalf("existing pre-commit hook did not run: %v", err)
	}
}

func TestConfiguredGitAuthorshipAppliesToCommitAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git process integration is covered separately on Windows CI")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)
	runGitCommand(t, gitPath, dir, "config", "alias.ci", "commit")

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--git-name", "Alias Agent",
		"--git-email", "alias@example.com",
		"--git-authorship", "configured",
		"--real-git", gitPath,
		"git", "-C", dir, "ci", "--allow-empty", "-m", "alias commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, message := commitIdentity(t, gitPath, dir)
	if identity != "Alias Agent|alias@example.com|Alias Agent|alias@example.com" || strings.TrimSpace(message) != "alias commit" {
		t.Fatalf("identity=%q message=%q", identity, message)
	}
}

func TestIncompleteConfiguredGitIdentityFallsBackToBot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git process integration is covered separately on Windows CI")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)
	keyPath := writeTestPrivateKey(t)
	server := botIdentityServer(t, "fallback-app", 24680)
	defer server.Close()

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", server.URL,
		"--git-name", "Incomplete Agent",
		"--git-authorship", "configured",
		"--real-git", gitPath,
		"--no-cache",
		"git", "-C", dir, "commit", "--allow-empty", "-m", "fallback commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, _ := commitIdentity(t, gitPath, dir)
	want := "fallback-app[bot]|24680+fallback-app[bot]@users.noreply.127.0.0.1|fallback-app[bot]|24680+fallback-app[bot]@users.noreply.127.0.0.1"
	if identity != want {
		t.Fatalf("identity=%q, want %q", identity, want)
	}
}

func TestExecGitShimUsesConfiguredAuthorship(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git process integration is covered separately on Windows CI")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--git-name", "Nested Agent",
		"--git-email", "nested@example.com",
		"--git-authorship", "configured",
		"--real-git", gitPath,
		"exec", "--", "git", "-C", dir, "commit", "--allow-empty", "-m", "nested commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, _ := commitIdentity(t, gitPath, dir)
	if identity != "Nested Agent|nested@example.com|Nested Agent|nested@example.com" {
		t.Fatalf("identity=%q", identity)
	}
}

func TestDisabledGitIdentityOverridePreservesClientConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git process integration is covered separately on Windows CI")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--override-git-identity=false",
		"--git-authorship", "bot",
		"--real-git", gitPath,
		"git", "-C", dir, "commit", "--allow-empty", "-m", "client commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, _ := commitIdentity(t, gitPath, dir)
	if identity != "Client User|client@example.com|Client User|client@example.com" {
		t.Fatalf("identity=%q", identity)
	}
}

func TestReadOnlyGitCommandDoesNotResolveBotIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git process integration is covered separately on Windows CI")
	}
	gitPath := realGit(t)
	dir := initializeIdentityRepository(t, gitPath)
	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", filepath.Join(t.TempDir(), "missing.pem"),
		"--real-git", gitPath,
		"git", "-C", dir, "status", "--short",
	)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func initializeIdentityRepository(t *testing.T, gitPath string) string {
	t.Helper()
	dir := t.TempDir()
	runGitCommand(t, gitPath, dir, "init")
	runGitCommand(t, gitPath, dir, "config", "user.name", "Client User")
	runGitCommand(t, gitPath, dir, "config", "user.email", "client@example.com")
	return dir
}

// realGit locates the underlying Git executable, skipping the viagh git shim
// when one is installed earlier on PATH. Tests must exercise the real Git
// binary rather than viagh itself, otherwise --real-git points back at viagh.
func realGit(t *testing.T) string {
	t.Helper()
	var viaghResolved string
	if viaghPath, err := exec.LookPath("viagh"); err == nil {
		if resolved, err := filepath.EvalSymlinks(viaghPath); err == nil {
			viaghResolved = resolved
		}
	}
	if gitPath, err := exec.LookPath("git"); err == nil {
		resolved, err := filepath.EvalSymlinks(gitPath)
		if err != nil || resolved != viaghResolved {
			return gitPath
		}
	}
	for _, candidate := range []string{
		"/usr/bin/git",
		"/bin/git",
		"/usr/local/bin/git",
		"/opt/homebrew/bin/git",
	} {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	t.Skip("real git binary not found")
	return ""
}

func botIdentityServer(t *testing.T, slug string, id int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app":
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": slug})
		case "/users/" + slug + "[bot]":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "login": slug + "[bot]"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func commitIdentity(t *testing.T, gitPath, dir string) (string, string) {
	t.Helper()
	cmd := exec.Command(gitPath, "-C", dir, "show", "-s", "--format=%an%x00%ae%x00%cn%x00%ce%x00%B", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(output), "\x00", 5)
	if len(parts) != 5 {
		t.Fatalf("unexpected commit output: %q", output)
	}
	return strings.Join(parts[:4], "|"), parts[4]
}
