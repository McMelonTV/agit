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
)

func TestOpenCodeTrailerAppendedToCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git hook integration is covered separately on Windows CI")
	}
	gitPath := realGitPath(t)
	dir := initializeIdentityRepository(t, gitPath)
	server := openCodeSessionServer(t, dir)
	defer server.Close()
	t.Setenv("OPENCODE", "1")
	t.Setenv("OPENCODE_PORT", openCodePort(t, server))
	t.Setenv("VIAGH_OPENCODE_TRAILER", "1")

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--git-name", "OpenCode Agent",
		"--git-email", "agent@example.com",
		"--git-authorship", "configured",
		"--real-git", gitPath,
		"git", "-C", dir, "commit", "--allow-empty", "-m", "opencode commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, message := commitIdentity(t, gitPath, dir)
	want := "Worked on by `opencode-go/deepseek-v4-flash:max` within `opencode 1.18.13`."
	if !strings.Contains(message, want) {
		t.Fatalf("message = %q, want trailer %q", message, want)
	}
}

func TestOpenCodeTrailerWithoutIdentityOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git hook integration is covered separately on Windows CI")
	}
	gitPath := realGitPath(t)
	dir := initializeIdentityRepository(t, gitPath)
	server := openCodeSessionServer(t, dir)
	defer server.Close()
	t.Setenv("OPENCODE", "1")
	t.Setenv("OPENCODE_PORT", openCodePort(t, server))
	t.Setenv("VIAGH_OPENCODE_TRAILER", "1")

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--override-git-identity=false",
		"--real-git", gitPath,
		"git", "-C", dir, "commit", "--allow-empty", "-m", "client commit",
	)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	identity, message := commitIdentity(t, gitPath, dir)
	if identity != "Client User|client@example.com|Client User|client@example.com" {
		t.Fatalf("identity=%q", identity)
	}
	if !strings.Contains(message, "Worked on by `opencode-go/deepseek-v4-flash:max` within `opencode 1.18.13`.") {
		t.Fatalf("message = %q, want opencode trailer", message)
	}
}

func TestOpenCodeTrailerOrderedBeforeCoauthor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git hook integration is covered separately on Windows CI")
	}
	gitPath := realGitPath(t)
	dir := initializeIdentityRepository(t, gitPath)
	server := openCodeSessionServer(t, dir)
	defer server.Close()
	t.Setenv("OPENCODE", "1")
	t.Setenv("OPENCODE_PORT", openCodePort(t, server))
	t.Setenv("VIAGH_OPENCODE_TRAILER", "1")

	keyPath := writeTestPrivateKey(t)
	botServer := botIdentityServer(t, "test-app", 12345)
	defer botServer.Close()

	stdout, stderr, code := runMainSubprocess(t, nil,
		"--app-id", "123",
		"--private-key", keyPath,
		"--api-url", botServer.URL,
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
	_, message := commitIdentity(t, gitPath, dir)
	opencodeTrailer := "Worked on by `opencode-go/deepseek-v4-flash:max` within `opencode 1.18.13`."
	coauthor := "Co-authored-by: test-app[bot] <12345+test-app[bot]@users.noreply.127.0.0.1>"
	opencodeIndex, coauthorIndex := strings.Index(message, opencodeTrailer), strings.Index(message, coauthor)
	if opencodeIndex < 0 || coauthorIndex < 0 || opencodeIndex > coauthorIndex {
		t.Fatalf("message=%q opencodeIndex=%d coauthorIndex=%d", message, opencodeIndex, coauthorIndex)
	}
}

func realGitPath(t *testing.T) string {
	t.Helper()
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
	t.Skip("real git not found at known locations")
	return ""
}

func openCodeSessionServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("directory") != filepath.Clean(dir) {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"model": map[string]any{
				"id":         "deepseek-v4-flash",
				"providerID": "opencode-go",
				"variant":    "max",
			},
			"version": "1.18.13",
			"time":    map[string]any{"updated": 200},
		}})
	}))
}

func openCodePort(t *testing.T, server *httptest.Server) string {
	t.Helper()
	hostport := strings.TrimPrefix(server.URL, "http://")
	_, port, ok := strings.Cut(hostport, ":")
	if !ok {
		t.Fatalf("unexpected server URL %q", server.URL)
	}
	return port
}
