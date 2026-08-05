package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
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
)

func writeTestPrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	path := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeExecutable(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func captureProcessOutput(t *testing.T, fn func() int) (stdout, stderr string, code int) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	code = fn()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	stdoutBytes, _ := io.ReadAll(stdoutReader)
	stderrBytes, _ := io.ReadAll(stderrReader)
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	return string(stdoutBytes), string(stderrBytes), code
}

func TestRepoListAcrossInstallationsEmitsEmptyJSONArray(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]githubapp.Installation{{ID: 1, Account: githubapp.Account{Login: "alpha"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/1/access_tokens":
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-1", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, "#!/bin/sh\nprintf '[]\\n'\n")
	cfg := testConfig(keyPath, server.URL)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runRepoListAcrossInstallations(cfg, &localBackend{cfg: cfg}, fakeGH, []string{"repo", "list", "--json", "nameWithOwner"}, "/bin/false", os.Environ())
	})
	if code != 0 || stderr != "" || stdout != "[]\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRepoListLimitAppliesToMergedResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]githubapp.Installation{
				{ID: 1, Account: githubapp.Account{Login: "alpha"}},
				{ID: 2, Account: githubapp.Account{Login: "beta"}},
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
  alpha:token-1) printf '[{"nameWithOwner":"alpha/z"},{"nameWithOwner":"alpha/a"}]\n' ;;
  beta:token-2) printf '[{"nameWithOwner":"beta/a"},{"nameWithOwner":"beta/z"}]\n' ;;
  *) exit 42 ;;
esac
`)
	cfg := testConfig(keyPath, server.URL)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runRepoListAcrossInstallations(cfg, &localBackend{cfg: cfg}, fakeGH, []string{"repo", "list", "--limit", "2", "--json", "nameWithOwner"}, "/bin/false", os.Environ())
	})
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var repositories []struct {
		Name string `json:"nameWithOwner"`
	}
	if err := json.Unmarshal([]byte(stdout), &repositories); err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 2 || repositories[0].Name != "alpha/z" || repositories[1].Name != "alpha/a" {
		t.Fatalf("unexpected merged limit result: %#v", repositories)
	}
}

func TestRepoListDoesNotEmitPartialStdoutOnLaterFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]githubapp.Installation{
				{ID: 1, Account: githubapp.Account{Login: "alpha"}},
				{ID: 2, Account: githubapp.Account{Login: "beta"}},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/access_tokens"):
			id := strings.Split(r.URL.Path, "/")[3]
			_ = json.NewEncoder(w).Encode(githubapp.Token{Token: "token-" + id, ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fakeGH := writeExecutable(t, `#!/bin/sh
case "$3" in
  alpha) printf 'alpha/one\n' ;;
  beta) echo 'beta failed' >&2; exit 52 ;;
esac
`)
	cfg := testConfig(keyPath, server.URL)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runRepoListAcrossInstallations(cfg, &localBackend{cfg: cfg}, fakeGH, []string{"repo", "list"}, "/bin/false", os.Environ())
	})
	if code != 52 || stdout != "" || !strings.Contains(stderr, "beta failed") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRepoListRejectsInvalidLimitBeforeAuthentication(t *testing.T) {
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runRepoListAcrossInstallations(config.Config{}, nil, "/missing/gh", []string{"repo", "list", "--limit", "bad"}, "/missing/viagh", nil)
	})
	if code != 1 || stdout != "" || !strings.Contains(stderr, "invalid --limit") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
