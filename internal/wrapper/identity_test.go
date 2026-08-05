package wrapper

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGitIdentityOverridesEnvironmentAndConfig(t *testing.T) {
	env, err := ApplyGitIdentity([]string{
		"GIT_AUTHOR_NAME=Client",
		"GIT_AUTHOR_EMAIL=client@example.com",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=user.name",
		"GIT_CONFIG_VALUE_0=Client",
	}, GitIdentity{Name: "Agent", Email: "agent@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"GIT_AUTHOR_NAME":     "Agent",
		"GIT_AUTHOR_EMAIL":    "agent@example.com",
		"GIT_COMMITTER_NAME":  "Agent",
		"GIT_COMMITTER_EMAIL": "agent@example.com",
		"GIT_CONFIG_COUNT":    "3",
		"GIT_CONFIG_KEY_1":    "user.name",
		"GIT_CONFIG_VALUE_1":  "Agent",
		"GIT_CONFIG_KEY_2":    "user.email",
		"GIT_CONFIG_VALUE_2":  "agent@example.com",
	} {
		if got, _ := LookupEnv(env, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestAppendCoauthorTrailerIsIdempotent(t *testing.T) {
	trailer := "Co-authored-by: test-app[bot] <1+test-app[bot]@users.noreply.github.com>"
	first := AppendCoauthorTrailer([]byte("Subject\n"), trailer)
	second := AppendCoauthorTrailer(first, trailer)
	if string(first) != string(second) {
		t.Fatalf("trailer was duplicated: %q", second)
	}
	if strings.Count(string(second), trailer) != 1 || !strings.Contains(string(second), "Subject\n\n"+trailer) {
		t.Fatalf("unexpected message: %q", second)
	}
}

func TestAppendCoauthorTrailerSupportsMultipleTrailers(t *testing.T) {
	opencode := "Worked on by `opencode-go/deepseek-v4-flash:max` within `opencode 1.18.13`."
	coauthor := "Co-authored-by: test-app[bot] <1+test-app[bot]@users.noreply.github.com>"
	message := AppendCoauthorTrailer([]byte("Subject\n"), opencode)
	message = AppendCoauthorTrailer(message, coauthor)
	want := "Subject\n\n" + opencode + "\n\n" + coauthor + "\n"
	if string(message) != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
	again := AppendCoauthorTrailer(AppendCoauthorTrailer(message, opencode), coauthor)
	if string(again) != string(message) {
		t.Fatalf("trailers were duplicated: %q", again)
	}
}

func TestSetOpenCodeTrailer(t *testing.T) {
	trailer := "Worked on by `provider/model:max` within `opencode 1.18.13`."
	env := SetOpenCodeTrailer([]string{"OPENCODE=1"}, trailer)
	if got, _ := LookupEnv(env, opencodeTrailerEnv); got != trailer {
		t.Fatalf("VIAGH_OPENCODE_TRAILER = %q, want %q", got, trailer)
	}
	env = SetOpenCodeTrailer(env, "")
	if _, ok := LookupEnv(env, opencodeTrailerEnv); ok {
		t.Fatal("VIAGH_OPENCODE_TRAILER was not unset")
	}
	if got := OpenCodeTrailerFromEnv(SetOpenCodeTrailer(nil, trailer)); got != trailer {
		t.Fatalf("OpenCodeTrailerFromEnv = %q, want %q", got, trailer)
	}
}

func TestGitTopLevel(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	cmd := exec.Command(gitPath, "-C", dir, "init")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	top, ok := GitTopLevel(gitPath, []string{"-C", filepath.Clean(dir), "commit", "-m", "x"}, os.Environ())
	if !ok {
		t.Fatal("GitTopLevel reported no top level")
	}
	// The returned path may differ textually from dir when symlinks or short
	// 8.3 names are involved (for example /var vs /private/var on macOS).
	// Verify it references the same repository through a marker file instead
	// of comparing path strings.
	marker := "viagh-git-toplevel-marker"
	if err := os.WriteFile(filepath.Join(dir, marker), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(top, marker)); err != nil {
		t.Fatalf("GitTopLevel %q does not reference the repository at %q: %v", top, dir, err)
	}
	if _, ok := GitTopLevel(gitPath, []string{"-C", filepath.Clean(t.TempDir()), "status"}, os.Environ()); ok {
		t.Fatal("GitTopLevel succeeded outside a repository")
	}
}

func TestGitMayCreateCommit(t *testing.T) {
	for _, args := range [][]string{
		{"commit", "-m", "message"},
		{"-C", "/tmp/repo", "merge", "topic"},
		{"--git-dir=.git", "cherry-pick", "HEAD"},
		{"commit-tree", "deadbeef"},
		{"tag", "-mrelease", "v1.0.0"},
		{"notes", "--ref", "review", "add", "HEAD"},
	} {
		if !GitMayCreateCommit(args) {
			t.Fatalf("expected commit-producing command: %v", args)
		}
	}
	for _, args := range [][]string{
		{"status"},
		{"fetch", "origin"},
		{"tag", "--list"},
		{"tag", "-lv1.*"},
		{"notes", "--ref", "review", "list"},
		{"--version"},
	} {
		if GitMayCreateCommit(args) {
			t.Fatalf("read-only command marked commit-producing: %v", args)
		}
	}
}

func TestAddCommitTreeTrailer(t *testing.T) {
	trailer := "Co-authored-by: Bot <bot@example.com>"
	args := AddCommitTreeTrailer([]string{"-C", "/tmp/repo", "commit-tree", "deadbeef"}, trailer)
	if got := strings.Join(args, "|"); got != "-C|/tmp/repo|commit-tree|deadbeef|-m|Co-authored-by: Bot <bot@example.com>" {
		t.Fatalf("args = %q", got)
	}
	unchanged := AddCommitTreeTrailer(args, trailer)
	if strings.Join(unchanged, "|") != strings.Join(args, "|") {
		t.Fatalf("trailer was duplicated: %v", unchanged)
	}
	attached := []string{"commit-tree", "deadbeef", "--message=" + trailer}
	if got := AddCommitTreeTrailer(attached, trailer); strings.Join(got, "|") != strings.Join(attached, "|") {
		t.Fatalf("attached trailer was duplicated: %v", got)
	}
}

func TestGitCommandIsAlias(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	cmd := exec.Command(gitPath, "-C", dir, "init")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	cmd = exec.Command(gitPath, "-C", dir, "config", "alias.ci", "commit")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config alias: %v: %s", err, output)
	}
	env := os.Environ()
	env = SetEnv(env, "GIT_CONFIG_NOSYSTEM", "1")
	env = SetEnv(env, "GIT_CONFIG_GLOBAL", os.DevNull)
	if !GitCommandIsAlias(gitPath, []string{"-C", filepath.Clean(dir), "ci"}, env) {
		t.Fatal("configured alias was not detected")
	}
	if GitCommandIsAlias(gitPath, []string{"-C", filepath.Clean(dir), "status"}, env) {
		t.Fatal("built-in command was incorrectly detected as an alias")
	}
}
