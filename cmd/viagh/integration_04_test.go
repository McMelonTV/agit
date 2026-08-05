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
)

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
