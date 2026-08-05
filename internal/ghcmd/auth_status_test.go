package ghcmd

import "testing"

func TestAuthStatusJSONHostsProbe(t *testing.T) {
	for _, args := range [][]string{
		{"auth", "status", "--json", "hosts"},
		{"auth", "status", "--json=hosts"},
		{"auth", "status", "--active", "--hostname", "github.com", "--json", "hosts"},
		{"auth", "status", "-a", "-hgithub.com", "--json=hosts"},
	} {
		if !IsAuthStatusJSONHosts(args) {
			t.Fatalf("expected probe detection for %v", args)
		}
		if !IsAuthStatus(args) {
			t.Fatalf("expected auth-status detection for %v", args)
		}
	}
	for _, args := range [][]string{
		{"auth", "status"},
		{"auth", "status", "--json", "hosts", "--show-token"},
		{"auth", "status", "--json", "hosts", "--jq", ".hosts"},
		{"auth", "status", "--json", "other"},
		{"auth", "status", "--help"},
		{"auth", "token", "--json", "hosts"},
	} {
		if IsAuthStatusJSONHosts(args) {
			t.Fatalf("unexpected probe detection for %v", args)
		}
	}

	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"auth", "status", "--hostname", "github.example.com", "--json", "hosts"}, "github.example.com"},
		{[]string{"auth", "status", "--hostname=github.example.com", "--json", "hosts"}, "github.example.com"},
		{[]string{"auth", "status", "-hgithub.example.com", "--json", "hosts"}, "github.example.com"},
	} {
		got, ok := AuthStatusHostname(test.args)
		if !ok || got != test.want {
			t.Fatalf("AuthStatusHostname(%v) = %q, %v; want %q, true", test.args, got, ok, test.want)
		}
	}
}

func TestAuthStatusPlainReport(t *testing.T) {
	for _, args := range [][]string{
		{"auth", "status"},
		{"auth", "status", "--hostname", "github.com"},
		{"auth", "status", "--hostname=github.com"},
		{"auth", "status", "-hgithub.com"},
		{"auth", "status", "--active"},
		{"auth", "status", "-a"},
		{"auth", "status", "-a", "-h", "github.com"},
	} {
		if !IsAuthStatus(args) {
			t.Fatalf("expected auth-status detection for %v", args)
		}
		if IsAuthStatusJSONHosts(args) {
			t.Fatalf("plain status %v must not be a JSON hosts probe", args)
		}
	}
	for _, args := range [][]string{
		{"auth", "status", "--show-token"},
		{"auth", "status", "-t"},
		{"auth", "status", "--jq", ".hosts"},
		{"auth", "status", "-q", ".hosts"},
		{"auth", "status", "--template", "{{.host}}"},
		{"auth", "status", "--json", "hosts", "--show-token"},
		{"auth", "status", "--json", "other"},
		{"auth", "status", "extra"},
		{"auth", "status", "--"},
		{"auth", "status", "--refresh"},
		{"auth", "status", "--help"},
		{"auth", "login"},
		{"auth", "token"},
		{"status"},
	} {
		if IsAuthStatus(args) {
			t.Fatalf("unexpected auth-status detection for %v", args)
		}
	}
}
