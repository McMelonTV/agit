package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/McMelonTV/agit/internal/githubapp"
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
if [ -n "$GHAPP_PRIVATE_KEY" ] || [ -n "$GHAPP_PRIVATE_KEY_PEM" ] || [ -n "$GHAPP_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
  echo "private key leaked" >&2
  exit 90
fi
case "$3:$GH_TOKEN" in
  acme/one:token-1) echo acme-ok ;;
  beta/two:token-2) echo beta-ok ;;
  *) echo "unexpected invocation: $* token=$GH_TOKEN" >&2; exit 91 ;;
esac
`)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
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
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
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
		"GHAPP_TEST_RUN_MAIN=1",
		"GHAPP_PRIVATE_KEY=",
		"GHAPP_PRIVATE_KEY_PEM=",
		"GHAPP_PRIVATE_KEY_BASE64=",
		"GITHUB_APP_PRIVATE_KEY=",
		"GHAPP_BROKER_URL=",
		"GHAPP_BROKER_SECRET=",
		"GHAPP_SESSION_CONFIG=",
		"GH_TOKEN=",
		"GITHUB_TOKEN=",
		"GH_ENTERPRISE_TOKEN=",
		"GITHUB_ENTERPRISE_TOKEN=",
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
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
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
if [ -n "$GHAPP_BROKER_URL" ] || [ -n "$GHAPP_BROKER_SECRET" ] || [ -n "$GHAPP_SESSION_CONFIG" ]; then
  echo "broker leaked to external host command" >&2
  exit 94
fi
printf 'external-env-host-ok\n'
`)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
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
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
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
if [ -n "$GHAPP_PRIVATE_KEY" ] || [ -n "$GHAPP_PRIVATE_KEY_PEM" ] || [ -n "$GHAPP_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
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
if [ -n "$GHAPP_PRIVATE_KEY" ] || [ -n "$GHAPP_PRIVATE_KEY_PEM" ] || [ -n "$GHAPP_PRIVATE_KEY_BASE64" ] || [ -n "$GITHUB_APP_PRIVATE_KEY" ]; then
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
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
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
