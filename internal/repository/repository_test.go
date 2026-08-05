package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input string
		host  string
		owner string
		repo  string
	}{
		{"https://github.com/acme/widgets.git", "github.com", "acme", "widgets"},
		{"git@github.com:acme/widgets.git", "github.com", "acme", "widgets"},
		{"ssh://git@github.example.com/acme/widgets.git", "github.example.com", "acme", "widgets"},
		{"acme/widgets", "", "acme", "widgets"},
		{"github.com/acme/widgets", "github.com", "acme", "widgets"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			ref, ok := Parse(test.input)
			if !ok {
				t.Fatal("Parse returned false")
			}
			if ref.Host != test.host || ref.Owner != test.owner || ref.Repo != test.repo {
				t.Fatalf("got %+v", ref)
			}
		})
	}
}

func TestParseRejectsAmbiguousOrEscapedPaths(t *testing.T) {
	for _, value := range []string{
		"https://github.com/prefix/acme/widgets",
		"https://github.com/acme%2Fother/widgets",
		"github.com/acme/widgets/extra",
	} {
		if ref, ok := Parse(value); ok {
			t.Fatalf("Parse(%q) unexpectedly returned %+v", value, ref)
		}
	}
}

func TestFromCredentialPath(t *testing.T) {
	for _, host := range []string{"github.com", "github.com:443"} {
		ref, ok := FromCredentialPath(host, "acme/widgets.git", "github.com")
		if !ok || ref.String() != "acme/widgets" {
			t.Fatalf("got %+v, %v for %s", ref, ok, host)
		}
	}
	if _, ok := FromCredentialPath("attacker.example", "acme/widgets.git", "github.com"); ok {
		t.Fatal("accepted a credential path for the wrong host")
	}
	if ref, ok := FromCredentialPath("ssh.github.com:443", "acme/widgets.git", "github.com"); !ok || ref.String() != "acme/widgets" {
		t.Fatalf("GitHub SSH-over-443 alias was not recognized: %+v, %v", ref, ok)
	}
}

func TestFromArgsRepoFlag(t *testing.T) {
	ref, ok := FromArgs("gh", []string{"pr", "list", "--repo", "acme/widgets"}, "github.com")
	if !ok || ref.String() != "acme/widgets" {
		t.Fatalf("got %+v, %v", ref, ok)
	}
	if _, ok := FromArgs("gh", []string{"extension", "--", "--repo", "evil/repo"}, "github.com"); ok {
		t.Fatal("parsed a repository flag after --")
	}
}

func TestFromArgsRepoCommands(t *testing.T) {
	for _, command := range []string{"edit", "unarchive", "archive", "clone", "delete", "fork", "set-default", "sync", "view"} {
		ref, ok := FromArgs("gh", []string{"repo", command, "acme/widgets"}, "github.com")
		if !ok || ref.String() != "acme/widgets" {
			t.Fatalf("command %s: got %+v, %v", command, ref, ok)
		}
	}
	if _, ok := FromArgs("gh", []string{"repo", "create", "acme/widgets"}, "github.com"); ok {
		t.Fatal("repo create target was mistaken for an existing repository")
	}
	if _, ok := FromArgs("gh", []string{"repo", "rename", "new-name"}, "github.com"); ok {
		t.Fatal("repo rename positional new name was mistaken for a target repository")
	}
	ref, ok := FromArgs("gh", []string{"repo", "rename", "new-name", "--repo", "acme/widgets"}, "github.com")
	if !ok || ref.String() != "acme/widgets" {
		t.Fatalf("rename --repo target not detected: %+v, %v", ref, ok)
	}
	for _, args := range [][]string{
		{"repo", "clone", "--upstream-remote-name", "upstream", "acme/widgets"},
		{"repo", "view", "--web", "acme/widgets"},
		{"repo", "edit", "--enable-issues", "acme/widgets"},
		{"repo", "edit", "--template", "acme/widgets"},
	} {
		ref, ok := FromArgs("gh", args, "github.com")
		if !ok || ref.String() != "acme/widgets" {
			t.Fatalf("flags-before-repository %v: got %+v, %v", args, ref, ok)
		}
	}
}

func TestFromRepoFlag(t *testing.T) {
	ref, ok := FromRepoFlag([]string{"pr", "list", "--repo=github.com/acme/widgets"}, "github.com")
	if !ok || ref.String() != "acme/widgets" {
		t.Fatalf("got %+v, %v", ref, ok)
	}
	if _, ok := FromRepoFlag([]string{"pr", "list", "--", "--repo", "acme/widgets"}, "github.com"); ok {
		t.Fatal("parsed --repo after --")
	}
	ref, ok = FromRepoFlag([]string{"pr", "list", "--repo", "alpha/one", "-Rbeta/two"}, "github.com")
	if !ok || ref.String() != "beta/two" {
		t.Fatalf("last --repo did not win: %+v, %v", ref, ok)
	}
}

func TestCurrentDirectoryArgsPreservesMultipleCOptions(t *testing.T) {
	want := []string{"-C", "parent", "-C", "child"}
	got := currentDirectoryArgs([]string{"-C", "parent", "-Cchild", "status"})
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestCurrentUsesBranchRemoteBeforeOrigin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses file URL paths")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	runGit(t, git, dir, "init")
	runGit(t, git, dir, "config", "user.name", "Test")
	runGit(t, git, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, git, dir, "add", "README")
	runGit(t, git, dir, "commit", "-m", "initial")
	runGit(t, git, dir, "remote", "add", "origin", "https://github.com/wrong/origin.git")
	runGit(t, git, dir, "remote", "add", "work", "https://github.com/acme/widgets.git")
	branch := gitOutput(context.Background(), git, []string{"-C", dir}, "symbolic-ref", "--short", "HEAD")
	runGit(t, git, dir, "config", "branch."+branch+".remote", "work")
	ref, ok := Current(context.Background(), git, []string{"-C", dir}, "github.com")
	if !ok || ref.String() != "acme/widgets" {
		t.Fatalf("got %+v, %v", ref, ok)
	}
}

func runGit(t *testing.T, git, dir string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command(git, commandArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestEmptyString(t *testing.T) {
	if got := (Ref{}).String(); got != "" {
		t.Fatalf("empty ref string = %q", got)
	}
}

func TestExplicitHost(t *testing.T) {
	for _, test := range []struct {
		tool string
		args []string
		want string
	}{
		{"gh", []string{"pr", "list", "--repo", "github.example.com/acme/widgets"}, "github.example.com"},
		{"gh", []string{"repo", "clone", "https://github.example.com/acme/widgets.git"}, "github.example.com"},
		{"gh", []string{"repo", "create", "github.example.com/acme/widgets"}, "github.example.com"},
		{"git", []string{"clone", "https://github.example.com/acme/widgets.git"}, "github.example.com"},
	} {
		got, ok := ExplicitHost(test.tool, test.args)
		if !ok || got != test.want {
			t.Fatalf("ExplicitHost(%s, %v) = %q, %v", test.tool, test.args, got, ok)
		}
	}
}

func TestCurrentUsesGHResolvedDefaultRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-like Git setup")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	runGit(t, git, dir, "init")
	runGit(t, git, dir, "remote", "add", "origin", "https://github.com/wrong/origin.git")
	runGit(t, git, dir, "remote", "add", "selected", "https://github.com/acme/widgets.git")
	runGit(t, git, dir, "config", "remote.selected.gh-resolved", "base")
	ref, ok := Current(context.Background(), git, []string{"-C", dir}, "github.com")
	if !ok || ref.String() != "acme/widgets" {
		t.Fatalf("got %+v, %v", ref, ok)
	}
}

func TestCurrentUsesRepoSetDefaultRemoteArgument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-like Git setup")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	runGit(t, git, dir, "init")
	runGit(t, git, dir, "remote", "add", "origin", "https://github.com/wrong/origin.git")
	runGit(t, git, dir, "remote", "add", "selected", "https://github.com/acme/widgets.git")
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	ref, ok := Current(context.Background(), git, []string{"repo", "set-default", "selected"}, "github.com")
	if !ok || ref.String() != "acme/widgets" {
		t.Fatalf("got %+v, %v", ref, ok)
	}
}

func TestCurrentAnyReturnsRepositoryFromDifferentHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-like Git setup")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	runGit(t, git, dir, "init")
	runGit(t, git, dir, "remote", "add", "origin", "https://github.other.example/acme/widgets.git")
	ref, ok := CurrentAny(context.Background(), git, []string{"-C", dir})
	if !ok || ref.Host != "github.other.example" || ref.String() != "acme/widgets" {
		t.Fatalf("got %+v, %v", ref, ok)
	}
	if _, ok := Current(context.Background(), git, []string{"-C", dir}, "github.com"); ok {
		t.Fatal("host-filtered lookup accepted a repository from another host")
	}
}
