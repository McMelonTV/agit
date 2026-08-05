package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"testing"
)

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
