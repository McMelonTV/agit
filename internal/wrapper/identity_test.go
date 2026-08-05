package wrapper

import (
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
	args := AddCommitTreeTrailer([]string{"-C", "/tmp/repo", "commit-tree", "deadbeef"}, "Co-authored-by: Bot <bot@example.com>")
	if got := strings.Join(args, "|"); got != "-C|/tmp/repo|commit-tree|deadbeef|-m|Co-authored-by: Bot <bot@example.com>" {
		t.Fatalf("args = %q", got)
	}
}
