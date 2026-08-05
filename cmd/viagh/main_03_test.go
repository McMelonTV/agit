package main

import (
	"os"
	"os/exec"

	"runtime"
	"strings"
	"testing"

	"github.com/McMelonTV/viagh/internal/config"
)

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
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
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
