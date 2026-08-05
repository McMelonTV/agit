package ghcmd

import "strings"

type authStatusMode int

const (
	authStatusNone authStatusMode = iota
	authStatusPlain
	authStatusJSONHosts
)

// IsAuthStatus identifies authentication probes that viagh answers itself:
// the plain `gh auth status` report and the machine-readable `--json hosts`
// variant used by clients such as T3 Code. Other auth commands and flags that
// display secrets or reformat output (--show-token, --json with other fields,
// --jq, --template) remain pass-through so viagh does not pretend to manage
// the real GitHub CLI credential store.
func IsAuthStatus(args []string) bool {
	return parseAuthStatus(args) != authStatusNone
}

// IsAuthStatusJSONHosts identifies the machine-readable authentication probe
// used by clients such as T3 Code.
func IsAuthStatusJSONHosts(args []string) bool {
	return parseAuthStatus(args) == authStatusJSONHosts
}

func parseAuthStatus(args []string) authStatusMode {
	if len(args) < 2 || args[0] != "auth" || args[1] != "status" {
		return authStatusNone
	}
	jsonHosts := false
	for index := 2; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			return authStatusNone
		}
		switch {
		case arg == "--json":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) != "hosts" {
				return authStatusNone
			}
			jsonHosts = true
			index++
		case strings.HasPrefix(arg, "--json="):
			if strings.TrimSpace(strings.TrimPrefix(arg, "--json=")) != "hosts" {
				return authStatusNone
			}
			jsonHosts = true
		case arg == "--hostname" || arg == "-h":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return authStatusNone
			}
			index++
		case strings.HasPrefix(arg, "--hostname="):
			if strings.TrimSpace(strings.TrimPrefix(arg, "--hostname=")) == "" {
				return authStatusNone
			}
		case strings.HasPrefix(arg, "-h") && len(arg) > 2:
			if strings.TrimSpace(strings.TrimPrefix(arg, "-h")) == "" {
				return authStatusNone
			}
		case arg == "--active" || arg == "-a":
			// A synthetic status contains exactly one active App account.
		default:
			// In particular, do not intercept --show-token, --jq, or
			// --template. Those retain the real gh command's behavior.
			return authStatusNone
		}
	}
	if jsonHosts {
		return authStatusJSONHosts
	}
	return authStatusPlain
}

// AuthStatusHostname returns the host selected by an intercepted auth-status
// probe. It accepts both the long and short gh flag forms.
func AuthStatusHostname(args []string) (string, bool) {
	if len(args) < 2 || args[0] != "auth" || args[1] != "status" {
		return "", false
	}
	for index := 2; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--hostname" || arg == "-h":
			if index+1 < len(args) {
				value := strings.TrimSpace(args[index+1])
				return value, value != ""
			}
			return "", false
		case strings.HasPrefix(arg, "--hostname="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--hostname="))
			return value, value != ""
		case strings.HasPrefix(arg, "-h") && len(arg) > 2:
			value := strings.TrimSpace(strings.TrimPrefix(arg, "-h"))
			return value, value != ""
		}
	}
	return "", false
}
