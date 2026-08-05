package main

import (
	"context"

	"encoding/json"

	"net/http"
	"net/http/httptest"
	"os"

	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/githubapp"
	"github.com/McMelonTV/viagh/internal/repository"
)

func TestRepoListAcrossInstallationsMergesJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}

	keyPath := writeTestPrivateKey(t)
	var mu sync.Mutex
	var tokenIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]githubapp.Installation{
				{ID: 2, Account: githubapp.Account{Login: "beta", Type: "Organization"}},
				{ID: 1, Account: githubapp.Account{Login: "alpha", Type: "Organization"}},
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/app/installations/") && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			parts := strings.Split(r.URL.Path, "/")
			id := parts[3]
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
owner="$3"
case "$owner:$GH_TOKEN" in
  alpha:token-1) printf '[{"nameWithOwner":"alpha/one"}]\n' ;;
  beta:token-2) printf '[{"nameWithOwner":"beta/two"}]\n' ;;
  *) echo "unexpected owner/token: $owner/$GH_TOKEN" >&2; exit 41 ;;
esac
`)

	cfg := testConfig(keyPath, server.URL)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runRepoListAcrossInstallations(cfg, &localBackend{cfg: cfg}, fakeGH, []string{"repo", "list", "--json", "nameWithOwner"}, "/bin/false", os.Environ())
	})
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	var repositories []map[string]any
	if err := json.Unmarshal([]byte(stdout), &repositories); err != nil {
		t.Fatalf("invalid merged JSON %q: %v", stdout, err)
	}
	if len(repositories) != 2 || repositories[0]["nameWithOwner"] != "alpha/one" || repositories[1]["nameWithOwner"] != "beta/two" {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(tokenIDs, ",") != "1,2" {
		t.Fatalf("token requests = %v, want [1 2]", tokenIDs)
	}
}

func TestRepoListAcrossInstallationsConcatenatesPlainOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}

	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]githubapp.Installation{
				{ID: 2, Account: githubapp.Account{Login: "beta", Type: "Organization"}},
				{ID: 1, Account: githubapp.Account{Login: "alpha", Type: "Organization"}},
			})
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
  alpha:token-1) printf 'alpha/one\n' ;;
  beta:token-2) printf 'beta/two\n' ;;
  *) exit 43 ;;
esac
`)

	cfg := testConfig(keyPath, server.URL)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runRepoListAcrossInstallations(cfg, &localBackend{cfg: cfg}, fakeGH, []string{"repo", "list"}, "/bin/false", os.Environ())
	})
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if stdout != "alpha/one\nbeta/two\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestExplicitRepoListOwnerSelectsInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}

	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/acme/installation":
			_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 9, Account: githubapp.Account{Login: "acme", Type: "Organization"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/9/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-9", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fakeGH := writeExecutable(t, `#!/bin/sh
if [ "$1 $2 $3" != "repo list acme" ] || [ "$GH_TOKEN" != "token-9" ]; then
  echo "unexpected invocation: $* token=$GH_TOKEN" >&2
  exit 42
fi
printf 'acme/one\n'
`)

	cfg := testConfig(keyPath, server.URL)
	cfg.RealGH = fakeGH
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runTool(cfg, "gh", []string{"repo", "list", "acme"})
	})
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if stdout != "acme/one\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestResolveInstallationUsesOwnerBeforeMultipleInstallations(t *testing.T) {
	keyPath := writeTestPrivateKey(t)
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := githubapp.LoadPrivateKey("", string(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/installation" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(githubapp.Installation{ID: 9, Account: githubapp.Account{Login: "acme"}})
	}))
	defer server.Close()

	client := githubapp.NewClient("123", key, server.URL, "2026-03-10", server.Client())
	installation, err := resolveInstallation(context.Background(), client, config.Config{}, repository.Ref{}, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if installation.ID != 9 {
		t.Fatalf("installation = %+v", installation)
	}
}

func testConfig(keyPath, apiURL string) config.Config {
	return config.Config{
		AppID:          "123",
		PrivateKeyPath: keyPath,
		Host:           "github.com",
		APIURL:         apiURL,
		APIVersion:     "2026-03-10",
		NoCache:        true,
		RefreshBefore:  5 * time.Minute,
		HTTPTimeout:    5 * time.Second,
	}
}
