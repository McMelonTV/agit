package ghcmd

import (
	"reflect"
	"testing"
)

func TestNeedsAuthentication(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--version"},
		{"repo"},
		{"pr"},
		{"issue"},
		{"workflow"},
		{"repo", "list", "--help"},
		{"completion", "bash"},
		{"config", "get", "editor"},
		{"alias", "list"},
		{"auth", "status"},
		{"repo", "set-default", "--view"},
		{"extension", "list"},
		{"ext", "rm", "example"},
	} {
		if NeedsAuthentication(args) {
			t.Fatalf("expected no authentication for %v", args)
		}
	}
	if !NeedsAuthentication([]string{"pr", "list", "--repo", "acme/widgets"}) {
		t.Fatal("expected authentication")
	}
	if !NeedsAuthentication([]string{"api", "--", "-h"}) {
		t.Fatal("argument after -- was mistaken for help")
	}
	if !NeedsAuthentication([]string{"extension", "upgrade", "example"}) {
		t.Fatal("networked extension command should authenticate")
	}
	for _, args := range [][]string{
		{"repo", "create", "-h", "https://example.com", "acme/widgets", "--private"},
		{"repo", "edit", "-h", "https://example.com", "acme/widgets"},
	} {
		if !NeedsAuthentication(args) {
			t.Fatalf("-h value flag was mistaken for help: %v", args)
		}
	}
}

func TestRepoListOwner(t *testing.T) {
	tests := []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"repo", "list", "acme"}, "acme", true},
		{[]string{"repo", "ls", "--limit", "100", "acme"}, "acme", true},
		{[]string{"repo", "list", "--json=nameWithOwner", "acme"}, "acme", true},
		{[]string{"repo", "list", "-L100", "acme"}, "acme", true},
		{[]string{"repo", "list", "--limit", "100"}, "", false},
		{[]string{"pr", "list", "--repo", "acme/widgets"}, "", false},
	}
	for _, test := range tests {
		got, ok := RepoListOwner(test.args)
		if got != test.want || ok != test.ok {
			t.Fatalf("RepoListOwner(%v) = %q, %v; want %q, %v", test.args, got, ok, test.want, test.ok)
		}
	}
}

func TestWithRepoListOwnerAndLimit(t *testing.T) {
	args := []string{"repo", "list", "--limit", "20"}
	want := []string{"repo", "list", "acme", "--limit", "20"}
	if got := WithRepoListOwner(args, "acme"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	want = []string{"repo", "list", "-L7"}
	if got := WithRepoListLimit([]string{"repo", "list", "-L20"}, 7); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRepoCreateOwnerQualification(t *testing.T) {
	args := []string{"repo", "create", "--description", "example", "widgets", "--private"}
	name, ok := RepoCreateUnqualifiedName(args)
	if !ok || name != "widgets" {
		t.Fatalf("name = %q, %v", name, ok)
	}
	want := []string{"repo", "create", "--description", "example", "acme/widgets", "--private"}
	if got := WithRepoCreateOwner(args, "acme"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	qualified := []string{"repo", "create", "acme/widgets", "--private"}
	if _, ok := RepoCreateUnqualifiedName(qualified); ok {
		t.Fatal("qualified repository was treated as unqualified")
	}
	if got := WithRepoCreateOwner(qualified, "other"); !reflect.DeepEqual(got, qualified) {
		t.Fatalf("qualified target was rewritten: %v", got)
	}
}

func TestRepoListRawJSONAndCompactFormatFlags(t *testing.T) {
	if !RepoListRawJSON([]string{"repo", "list", "--json", "nameWithOwner"}) {
		t.Fatal("expected raw JSON")
	}
	for _, args := range [][]string{
		{"repo", "list", "--json", "nameWithOwner", "--jq", ".[].nameWithOwner"},
		{"repo", "list", "--json", "nameWithOwner", "-q.[].nameWithOwner"},
		{"repo", "list", "--json", "nameWithOwner", "-t{{.name}}"},
	} {
		if RepoListRawJSON(args) || !RepoListHasFormatter(args) {
			t.Fatalf("formatted output was treated as raw JSON: %v", args)
		}
	}
}

func TestRepoListLimit(t *testing.T) {
	if got, err := RepoListLimit([]string{"repo", "list"}); err != nil || got != 30 {
		t.Fatalf("default limit = %d, %v", got, err)
	}
	if got, err := RepoListLimit([]string{"repo", "list", "-L42"}); err != nil || got != 42 {
		t.Fatalf("compact limit = %d, %v", got, err)
	}
	for _, args := range [][]string{
		{"repo", "list", "--limit", "0"},
		{"repo", "list", "--limit", "bad"},
		{"repo", "list", "--limit"},
		{"repo", "list", "-L"},
	} {
		if _, err := RepoListLimit(args); err == nil {
			t.Fatalf("expected invalid limit error for %v", args)
		}
	}
}

func TestWithRepoListLimitInsertsBeforeDoubleDash(t *testing.T) {
	want := []string{"repo", "list", "--limit", "7", "--", "literal"}
	if got := WithRepoListLimit([]string{"repo", "list", "--", "literal"}, 7); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOwner(t *testing.T) {
	tests := []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"repo", "list", "acme"}, "acme", true},
		{[]string{"project", "list", "--owner", "acme"}, "acme", true},
		{[]string{"codespace", "list", "-o", "acme"}, "acme", true},
		{[]string{"codespace", "view", "--repo-owner", "acme"}, "acme", true},
		{[]string{"secret", "list", "-o", "acme"}, "acme", true},
		{[]string{"api", "--hostname", "github.com", "/orgs/acme/repos"}, "acme", true},
		{[]string{"api", "https://github.example/api/v3/repos/acme/widgets/issues"}, "acme", true},
		{[]string{"repo", "create", "acme/widgets", "--private"}, "acme", true},
		{[]string{"repo", "create", "--description", "example", "acme/widgets"}, "acme", true},
		{[]string{"repo", "fork", "source/widgets", "--org", "target"}, "target", true},
		{[]string{"repo", "fork", "--org=target", "source/widgets"}, "target", true},
		{[]string{"project", "list", "--owner", "@me"}, "", false},
		{[]string{"search", "repos", "--owner", "acme", "--owner", "other"}, "", false},
		{[]string{"extension-name", "--owner", "acme"}, "", false},
		{[]string{"project", "list", "--", "--owner", "acme"}, "", false},
	}
	for _, test := range tests {
		got, ok := Owner(test.args)
		if got != test.want || ok != test.ok {
			t.Fatalf("Owner(%v) = %q, %v; want %q, %v", test.args, got, ok, test.want, test.ok)
		}
	}
}

func TestTargetHostname(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"api", "--hostname", "github.example.com", "/user"}, "github.example.com"},
		{[]string{"api", "--hostname=github.example.com", "/user"}, "github.example.com"},
		{[]string{"api", "https://github.example.com/api/v3/user"}, "github.example.com"},
	} {
		got, ok := TargetHostname(test.args)
		if !ok || got != test.want {
			t.Fatalf("TargetHostname(%v) = %q, %v", test.args, got, ok)
		}
	}
	got, ok := TargetHostname([]string{"api", "--hostname", "first.example", "--hostname=second.example", "/user"})
	if !ok || got != "second.example" {
		t.Fatalf("last hostname did not win: %q, %v", got, ok)
	}
}
