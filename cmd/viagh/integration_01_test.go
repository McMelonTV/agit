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
