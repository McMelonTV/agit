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
