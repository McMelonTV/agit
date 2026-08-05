package repository

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Ref struct {
	Host  string `json:"host,omitempty"`
	Owner string `json:"owner,omitempty"`
	Repo  string `json:"repo,omitempty"`
}

func (r Ref) String() string {
	if r.Owner == "" || r.Repo == "" {
		return ""
	}
	return r.Owner + "/" + r.Repo
}

func Parse(value string) (Ref, bool) {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "'\"")
	if value == "" {
		return Ref{}, false
	}

	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return Ref{}, false
		}
		return fromPath(parsed.Host, parsed.Path)
	}
	if match := scpPattern.FindStringSubmatch(value); match != nil {
		return fromPath(match[1], match[2])
	}
	value = strings.TrimPrefix(value, "/")
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		return clean(Ref{Owner: parts[0], Repo: parts[1]})
	}
	if len(parts) == 3 {
		return clean(Ref{Host: parts[0], Owner: parts[1], Repo: parts[2]})
	}
	return Ref{}, false
}

var scpPattern = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

func fromPath(host, path string) (Ref, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return Ref{}, false
	}
	owner, err := url.PathUnescape(parts[0])
	if err != nil {
		return Ref{}, false
	}
	repo, err := url.PathUnescape(parts[1])
	if err != nil {
		return Ref{}, false
	}
	return clean(Ref{Host: host, Owner: owner, Repo: repo})
}

func clean(ref Ref) (Ref, bool) {
	ref.Host = strings.ToLower(strings.TrimSpace(ref.Host))
	ref.Owner = strings.TrimSpace(ref.Owner)
	ref.Repo = strings.TrimSpace(ref.Repo)
	if index := strings.IndexAny(ref.Repo, "#?"); index >= 0 {
		ref.Repo = ref.Repo[:index]
	}
	ref.Repo = strings.TrimSuffix(ref.Repo, ".git")
	if ref.Owner == "" || ref.Repo == "" || ref.Owner == "." || ref.Owner == ".." || ref.Repo == "." || ref.Repo == ".." {
		return Ref{}, false
	}
	if strings.ContainsAny(ref.Owner, "/\\") || strings.ContainsAny(ref.Repo, "/\\") {
		return Ref{}, false
	}
	return ref, true
}

func FromCredentialPath(host, path, expectedHost string) (Ref, bool) {
	if strings.TrimSpace(host) == "" || !hostMatches(host, expectedHost) {
		return Ref{}, false
	}
	return fromPath(host, path)
}

func ExplicitHost(tool string, args []string) (string, bool) {
	if ref, ok := fromRepoFlag(args); ok && ref.Host != "" {
		return ref.Host, true
	}
	if tool == "git" {
		for _, arg := range args {
			if !looksLikeRemote(arg) {
				continue
			}
			if ref, ok := Parse(arg); ok && ref.Host != "" {
				return ref.Host, true
			}
		}
	}
	if tool == "gh" && len(args) >= 2 && args[0] == "repo" {
		if args[1] == "create" || args[1] == "new" {
			if ref, ok := repoCreateTarget(args[2:]); ok && ref.Host != "" {
				return ref.Host, true
			}
		}
		if ref, ok := repoCommandRepository(args[1], args[2:]); ok && ref.Host != "" {
			return ref.Host, true
		}
	}
	return "", false
}

func repoCreateTarget(args []string) (Ref, bool) {
	valueFlags := map[string]bool{
		"--description": true, "-d": true,
		"--gitignore": true, "-g": true,
		"--homepage": true, "-h": true,
		"--license": true, "-l": true,
		"--remote": true, "-r": true,
		"--source": true, "-s": true,
		"--team": true, "-t": true,
		"--template": true, "-p": true,
	}
	for _, value := range positionalArgs(args, valueFlags) {
		if ref, ok := Parse(value); ok {
			return ref, true
		}
	}
	return Ref{}, false
}

func HostMatches(actual, expected string) bool {
	return hostMatches(actual, expected)
}

// FromRepoFlag returns an explicit -R/--repo selector when it targets the
// configured GitHub host. Explicit repository flags take precedence over
// owner inference and ambient repository settings.
func FromRepoFlag(args []string, expectedHost string) (Ref, bool) {
	ref, ok := fromRepoFlag(args)
	if !ok || !hostMatches(ref.Host, expectedHost) {
		return Ref{}, false
	}
	return ref, true
}

func FromArgs(tool string, args []string, expectedHost string) (Ref, bool) {
	if ref, ok := FromRepoFlag(args, expectedHost); ok {
		return ref, true
	}

	if tool == "git" {
		for _, arg := range args {
			if arg == "--" {
				continue
			}
			if !looksLikeRemote(arg) {
				continue
			}
			if ref, ok := Parse(arg); ok && hostMatches(ref.Host, expectedHost) {
				return ref, true
			}
		}
	}

	if tool == "gh" && len(args) >= 2 && args[0] == "repo" {
		if ref, ok := repoCommandRepository(args[1], args[2:]); ok && hostMatches(ref.Host, expectedHost) {
			return ref, true
		}
	}
	return Ref{}, false
}

func repoCommandRepository(command string, args []string) (Ref, bool) {
	if !repoCommandAcceptsRepository(command) {
		return Ref{}, false
	}
	for _, value := range positionalArgs(args, repoCommandValueFlags(command)) {
		if ref, ok := Parse(value); ok {
			return ref, true
		}
	}
	return Ref{}, false
}

func repoCommandValueFlags(command string) map[string]bool {
	flags := make(map[string]bool)
	var names []string
	switch command {
	case "clone":
		names = []string{"--upstream-remote-name", "-u"}
	case "edit":
		names = []string{
			"--add-topic", "--default-branch", "--description", "-d",
			"--homepage", "-h", "--remove-topic",
			"--squash-merge-commit-message", "--visibility",
		}
	case "fork":
		names = []string{"--fork-name", "--org", "--remote-name"}
	case "sync":
		names = []string{"--branch", "-b", "--source", "-s"}
	case "view":
		names = []string{"--branch", "-b", "--jq", "-q", "--json", "--template", "-t"}
	}
	for _, name := range names {
		flags[name] = true
	}
	return flags
}

func positionalArgs(args []string, valueFlags map[string]bool) []string {
	var values []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			values = append(values, args[index+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, _, attached := strings.Cut(arg, "=")
			if valueFlags[name] && !attached && index+1 < len(args) {
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			name := arg
			if len(arg) > 2 {
				name = arg[:2]
			}
			if valueFlags[name] && len(arg) == 2 && index+1 < len(args) {
				index++
			}
			continue
		}
		values = append(values, arg)
	}
	return values
}

func repoCommandAcceptsRepository(command string) bool {
	switch command {
	case "archive", "clone", "delete", "edit", "fork", "set-default", "sync", "unarchive", "view":
		return true
	default:
		return false
	}
}

func Current(ctx context.Context, gitPath string, gitArgs []string, expectedHost string) (Ref, bool) {
	return current(ctx, gitPath, gitArgs, func(ref Ref) bool {
		return hostMatches(ref.Host, expectedHost)
	})
}

// CurrentAny returns the repository selected by GitHub CLI's remote-selection
// conventions without filtering by host. Callers use it to detect when the
// current checkout belongs to another GitHub host and should retain that
// host's existing user authentication rather than receiving App credentials.
func CurrentAny(ctx context.Context, gitPath string, gitArgs []string) (Ref, bool) {
	return current(ctx, gitPath, gitArgs, func(Ref) bool { return true })
}

func current(ctx context.Context, gitPath string, gitArgs []string, accept func(Ref) bool) (Ref, bool) {
	if gitPath == "" {
		return Ref{}, false
	}
	prefix := currentDirectoryArgs(gitArgs)
	for _, remote := range candidateRemotes(ctx, gitPath, prefix, gitArgs) {
		args := append(append([]string(nil), prefix...), "remote", "get-url", remote)
		cmd := exec.CommandContext(ctx, gitPath, args...)
		cmd.Stderr = nil
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		ref, ok := Parse(strings.TrimSpace(string(output)))
		if ok && accept(ref) {
			return ref, true
		}
	}
	return Ref{}, false
}

func candidateRemotes(ctx context.Context, gitPath string, prefix, gitArgs []string) []string {
	var candidates []string
	candidates = append(candidates, ghResolvedRemotes(ctx, gitPath, prefix)...)
	if selected := ghSetDefaultRemote(gitArgs); selected != "" {
		candidates = append([]string{selected}, candidates...)
	}
	if explicit := explicitRemoteName(gitArgs); explicit != "" {
		candidates = append(candidates, explicit)
	}
	commandIndex := gitCommandIndex(gitArgs)
	if commandIndex >= 0 && gitArgs[commandIndex] == "push" {
		if value := gitOutput(ctx, gitPath, prefix, "config", "--get", "remote.pushDefault"); value != "" {
			candidates = append(candidates, value)
		}
	}
	branch := gitOutput(ctx, gitPath, prefix, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branch != "" {
		if value := gitOutput(ctx, gitPath, prefix, "config", "--get", "branch."+branch+".remote"); value != "" && value != "." {
			candidates = append(candidates, value)
		}
	}
	candidates = append(candidates, "origin", "upstream")
	if output := gitOutput(ctx, gitPath, prefix, "remote"); output != "" {
		candidates = append(candidates, strings.Fields(output)...)
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	return result
}

func ghResolvedRemotes(ctx context.Context, gitPath string, prefix []string) []string {
	output := gitOutput(ctx, gitPath, prefix, "config", "--get-regexp", `^remote\..*\.gh-resolved$`)
	var remotes []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[len(fields)-1] != "base" {
			continue
		}
		key := fields[0]
		const keyPrefix = "remote."
		const keySuffix = ".gh-resolved"
		if strings.HasPrefix(key, keyPrefix) && strings.HasSuffix(key, keySuffix) {
			name := strings.TrimSuffix(strings.TrimPrefix(key, keyPrefix), keySuffix)
			if name != "" {
				remotes = append(remotes, name)
			}
		}
	}
	return remotes
}

func ghSetDefaultRemote(args []string) string {
	if len(args) < 3 || args[0] != "repo" || args[1] != "set-default" {
		return ""
	}
	for _, arg := range args[2:] {
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if _, ok := Parse(arg); !ok {
			return arg
		}
		return ""
	}
	return ""
}

func gitOutput(ctx context.Context, gitPath string, prefix []string, args ...string) string {
	commandArgs := append(append([]string(nil), prefix...), args...)
	cmd := exec.CommandContext(ctx, gitPath, commandArgs...)
	cmd.Stderr = nil
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func currentDirectoryArgs(args []string) []string {
	commandIndex := gitCommandIndex(args)
	if commandIndex < 0 {
		commandIndex = len(args)
	}
	var result []string
	for index := 0; index < commandIndex; index++ {
		if args[index] == "-C" {
			if index+1 < commandIndex {
				result = append(result, "-C", args[index+1])
				index++
			}
			continue
		}
		if strings.HasPrefix(args[index], "-C") && len(args[index]) > 2 {
			result = append(result, "-C", strings.TrimPrefix(args[index], "-C"))
		}
	}
	return result
}

func explicitRemoteName(args []string) string {
	commandIndex := gitCommandIndex(args)
	if commandIndex < 0 {
		return ""
	}
	command := args[commandIndex]
	if command != "fetch" && command != "pull" && command != "push" {
		return ""
	}
	valueFlags := map[string]bool{
		"--depth": true, "--deepen": true, "--shallow-since": true, "--shallow-exclude": true,
		"--jobs": true, "-j": true, "--recurse-submodules": true, "--upload-pack": true,
		"--receive-pack": true, "--repo": true, "--exec": true,
	}
	for index := commandIndex + 1; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if index+1 < len(args) {
				arg = args[index+1]
			} else {
				return ""
			}
		}
		if strings.HasPrefix(arg, "--") {
			name, _, attached := strings.Cut(arg, "=")
			if valueFlags[name] && !attached {
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if valueFlags[arg] {
				index++
			}
			continue
		}
		if _, ok := Parse(arg); ok || looksLikeRemote(arg) {
			return ""
		}
		return arg
	}
	return ""
}

func gitCommandIndex(args []string) int {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-C", arg == "-c", arg == "--git-dir", arg == "--work-tree", arg == "--namespace", arg == "--super-prefix", arg == "--config-env":
			index++
		case arg == "--":
			if index+1 < len(args) {
				return index + 1
			}
			return -1
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return index
		}
	}
	return -1
}

func fromRepoFlag(args []string) (Ref, bool) {
	var selected Ref
	found := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		switch {
		case arg == "-R" || arg == "--repo":
			if index+1 < len(args) {
				selected, found = Parse(args[index+1])
				index++
			}
		case strings.HasPrefix(arg, "--repo="):
			selected, found = Parse(strings.TrimPrefix(arg, "--repo="))
		case strings.HasPrefix(arg, "-R") && len(arg) > 2:
			selected, found = Parse(strings.TrimPrefix(arg, "-R"))
		}
	}
	return selected, found
}

func looksLikeRemote(value string) bool {
	return strings.Contains(value, "://") || strings.HasPrefix(value, "git@") || strings.HasSuffix(value, ".git")
}

func hostMatches(actual, expected string) bool {
	if actual == "" || strings.EqualFold(actual, expected) {
		return true
	}
	actualHost, actualPort := splitHostPort(actual)
	expectedHost, expectedPort := splitHostPort(expected)
	if strings.EqualFold(expectedHost, "github.com") && strings.EqualFold(actualHost, "ssh.github.com") && actualPort == "443" {
		return normalizeHTTPSPort(expectedPort) == "443"
	}
	return strings.EqualFold(actualHost, expectedHost) && normalizeHTTPSPort(actualPort) == normalizeHTTPSPort(expectedPort)
}

func splitHostPort(value string) (string, string) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return value, ""
	}
	return host, port
}

func normalizeHTTPSPort(port string) string {
	if port == "" || port == "443" {
		return "443"
	}
	return port
}

func ResolvePath(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(value, "~/"))
		}
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return absolute
}

func Validate(ref Ref) error {
	if ref.Owner == "" || ref.Repo == "" {
		return fmt.Errorf("repository must be in OWNER/REPO form")
	}
	return nil
}
