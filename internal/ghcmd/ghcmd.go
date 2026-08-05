package ghcmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const defaultRepoListLimit = 30

func NeedsAuthentication(args []string) bool {
	if len(args) == 0 || hasHelpFlag(args) {
		return false
	}
	if len(args) == 1 && commandGroups[args[0]] {
		return false
	}
	switch args[0] {
	case "help", "version", "--version", "completion", "config", "alias", "auth":
		return false
	case "repo":
		return len(args) < 2 || args[1] != "set-default"
	case "extension", "ext":
		return len(args) < 2 || (args[1] != "list" && args[1] != "remove" && args[1] != "rm")
	default:
		return true
	}
}

var commandGroups = map[string]bool{
	"agent-task": true, "api": true, "attestation": true, "cache": true,
	"codespace": true, "discussion": true, "extension": true, "ext": true,
	"gist": true, "gpg-key": true, "issue": true, "label": true,
	"org": true, "pr": true, "preview": true, "project": true,
	"release": true, "repo": true, "ruleset": true, "rs": true,
	"run": true, "search": true, "secret": true, "skill": true,
	"skills": true, "ssh-key": true, "variable": true, "workflow": true,
}

func IsRepoList(args []string) bool {
	return len(args) >= 2 && args[0] == "repo" && (args[1] == "list" || args[1] == "ls")
}

func RepoCreateUnqualifiedName(args []string) (string, bool) {
	index, value, ok := repoCreatePositional(args)
	_ = index
	if !ok || strings.Contains(value, "/") {
		return "", false
	}
	return value, true
}

func WithRepoCreateOwner(args []string, owner string) []string {
	index, value, ok := repoCreatePositional(args)
	if !ok || owner == "" || strings.Contains(value, "/") {
		return append([]string(nil), args...)
	}
	result := append([]string(nil), args...)
	result[index] = owner + "/" + value
	return result
}

func repoCreatePositional(args []string) (int, string, bool) {
	if len(args) < 2 || args[0] != "repo" || (args[1] != "create" && args[1] != "new") {
		return 0, "", false
	}
	for index := 2; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if index+1 < len(args) {
				return index + 1, args[index+1], true
			}
			return 0, "", false
		}
		if strings.HasPrefix(arg, "--") {
			name, _, attached := strings.Cut(arg, "=")
			if repoCreateValueFlags[name] && !attached && index+1 < len(args) {
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			name := arg
			if len(arg) > 2 {
				name = arg[:2]
			}
			if repoCreateValueFlags[name] && len(arg) == 2 && index+1 < len(args) {
				index++
			}
			continue
		}
		return index, arg, true
	}
	return 0, "", false
}

func RepoListOwner(args []string) (string, bool) {
	if !IsRepoList(args) {
		return "", false
	}
	positionals := positionalArgs(args[2:], repoListValueFlags)
	if len(positionals) == 0 {
		return "", false
	}
	owner := cleanOwner(positionals[0])
	return owner, owner != ""
}

func WithRepoListOwner(args []string, owner string) []string {
	if !IsRepoList(args) || owner == "" {
		return append([]string(nil), args...)
	}
	result := make([]string, 0, len(args)+1)
	result = append(result, args[:2]...)
	result = append(result, owner)
	result = append(result, args[2:]...)
	return result
}

func RepoListRawJSON(args []string) bool {
	return IsRepoList(args) && flagPresent(args[2:], "--json") && !RepoListHasFormatter(args)
}

func RepoListHasFormatter(args []string) bool {
	if !IsRepoList(args) {
		return false
	}
	return flagPresent(args[2:], "--jq", "-q", "--template", "-t")
}

func RepoListLimit(args []string) (int, error) {
	if !IsRepoList(args) {
		return defaultRepoListLimit, nil
	}
	value, found, err := repoListLimitValue(args[2:])
	if err != nil {
		return 0, err
	}
	if !found {
		return defaultRepoListLimit, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid --limit value %q: must be a positive integer", value)
	}
	return parsed, nil
}

func repoListLimitValue(args []string) (string, bool, error) {
	var selected string
	found := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		switch {
		case arg == "--limit" || arg == "-L":
			if index+1 >= len(args) || args[index+1] == "--" {
				return "", false, fmt.Errorf("flag %s requires a value", arg)
			}
			selected, found = args[index+1], true
			index++
		case strings.HasPrefix(arg, "--limit="):
			selected, found = strings.TrimPrefix(arg, "--limit="), true
		case strings.HasPrefix(arg, "-L") && len(arg) > 2:
			selected, found = arg[2:], true
		}
	}
	return selected, found, nil
}

func WithRepoListLimit(args []string, limit int) []string {
	if !IsRepoList(args) || limit <= 0 {
		return append([]string(nil), args...)
	}
	result := make([]string, 0, len(args)+2)
	replaced := false
	inserted := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if !replaced {
				result = append(result, "--limit", strconv.Itoa(limit))
				inserted = true
			}
			result = append(result, args[index:]...)
			break
		}
		switch {
		case arg == "--limit" || arg == "-L":
			result = append(result, arg, strconv.Itoa(limit))
			replaced = true
			if index+1 < len(args) {
				index++
			}
		case strings.HasPrefix(arg, "--limit="):
			result = append(result, "--limit="+strconv.Itoa(limit))
			replaced = true
		case strings.HasPrefix(arg, "-L") && len(arg) > 2:
			result = append(result, "-L"+strconv.Itoa(limit))
			replaced = true
		default:
			result = append(result, arg)
		}
	}
	if !replaced && !inserted {
		result = append(result, "--limit", strconv.Itoa(limit))
	}
	return result
}

func TargetHostname(args []string) (string, bool) {
	var selected string
	found := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if arg == "--hostname" {
			if index+1 < len(args) {
				selected = strings.TrimSpace(args[index+1])
				found = selected != ""
				index++
				continue
			}
			selected, found = "", false
			continue
		}
		if strings.HasPrefix(arg, "--hostname=") {
			selected = strings.TrimSpace(strings.TrimPrefix(arg, "--hostname="))
			found = selected != ""
		}
	}
	if found {
		return selected, true
	}
	if len(args) >= 2 && args[0] == "api" && args[1] != "graphql" {
		if endpoint := firstAPIEndpoint(args[1:]); endpoint != "" {
			if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
				return parsed.Host, true
			}
		}
	}
	return "", false
}

func Owner(args []string) (string, bool) {
	if owner, ok := RepoListOwner(args); ok {
		return owner, true
	}
	if owner, ok := repoMutationOwner(args); ok {
		return owner, true
	}
	if owner, ok := positionalOrgOwner(args); ok {
		return owner, true
	}
	if owner, ok := commandOwnerFlag(args); ok {
		return owner, true
	}
	return apiOwner(args)
}

func repoMutationOwner(args []string) (string, bool) {
	if len(args) < 2 || args[0] != "repo" {
		return "", false
	}
	switch args[1] {
	case "create", "new":
		_, target, ok := repoCreatePositional(args)
		if !ok {
			return "", false
		}
		parts := strings.Split(strings.Trim(target, "/"), "/")
		if len(parts) != 2 {
			return "", false
		}
		owner := cleanOwner(parts[0])
		return owner, owner != ""
	case "fork":
		value, ok := lastFlagValue(args[2:], "--org")
		owner := cleanOwner(value)
		return owner, ok && owner != ""
	default:
		return "", false
	}
}

var repoCreateValueFlags = map[string]bool{
	"--description": true, "-d": true,
	"--gitignore": true, "-g": true,
	"--homepage": true, "-h": true,
	"--license": true, "-l": true,
	"--remote": true, "-r": true,
	"--source": true, "-s": true,
	"--team": true, "-t": true,
	"--template": true, "-p": true,
}

func commandOwnerFlag(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	var names []string
	switch args[0] {
	case "project":
		names = []string{"--owner"}
	case "secret", "variable", "ruleset", "rs", "codespace", "cs":
		names = []string{"--org", "-o", "--repo-owner"}
	case "search":
		if len(args) >= 2 && (args[1] == "repos" || args[1] == "commits" || args[1] == "issues" || args[1] == "prs" || args[1] == "code") {
			names = []string{"--owner"}
		}
	case "skill", "skills":
		if len(args) >= 2 && args[1] == "search" {
			names = []string{"--owner"}
		}
	}
	var owners []string
	for _, name := range names {
		owners = append(owners, flagValues(args, name)...)
	}
	return oneOwner(owners)
}

func positionalOrgOwner(args []string) (string, bool) {
	if len(args) < 3 || args[0] != "org" {
		return "", false
	}
	switch args[1] {
	case "view", "edit":
		owner := cleanOwner(args[2])
		return owner, owner != ""
	default:
		return "", false
	}
}

func apiOwner(args []string) (string, bool) {
	if len(args) < 2 || args[0] != "api" || args[1] == "graphql" {
		return "", false
	}
	endpoint := firstAPIEndpoint(args[1:])
	if endpoint == "" {
		return "", false
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Path != "" {
		endpoint = parsed.Path
	}
	parts := strings.Split(strings.Trim(endpoint, "/"), "/")
	for index, part := range parts {
		switch part {
		case "orgs", "users":
			if index+1 < len(parts) {
				owner := cleanOwner(parts[index+1])
				return owner, owner != ""
			}
		case "repos":
			if index+2 < len(parts) {
				owner := cleanOwner(parts[index+1])
				return owner, owner != ""
			}
		}
	}
	return "", false
}

func firstAPIEndpoint(args []string) string {
	valueFlags := map[string]bool{
		"--hostname": true, "--method": true, "-X": true, "--header": true, "-H": true,
		"--field": true, "-F": true, "--raw-field": true, "-f": true,
		"--input": true, "--cache": true, "--preview": true, "--jq": true,
		"-q": true, "--template": true, "-t": true,
	}
	positionals := positionalArgs(args, valueFlags)
	if len(positionals) == 0 {
		return ""
	}
	return positionals[0]
}

var repoListValueFlags = map[string]bool{
	"--jq": true, "-q": true, "--json": true, "--language": true, "-l": true,
	"--limit": true, "-L": true, "--template": true, "-t": true,
	"--topic": true, "--visibility": true,
}

func positionalArgs(args []string, valueFlags map[string]bool) []string {
	var result []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			result = append(result, args[index+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, _, attached := strings.Cut(arg, "=")
			if valueFlags[name] && !attached {
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			name := arg
			if len(arg) > 2 {
				name = arg[:2]
			}
			if valueFlags[name] && len(arg) == 2 {
				index++
			}
			continue
		}
		result = append(result, arg)
	}
	return result
}

func flagPresent(args []string, names ...string) bool {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		name := arg
		if before, _, found := strings.Cut(arg, "="); found {
			name = before
		} else if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
			name = arg[:2]
		}
		if wanted[name] {
			return true
		}
	}
	return false
}

func lastFlagValue(args []string, names ...string) (string, bool) {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	var selected string
	found := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if before, after, attached := strings.Cut(arg, "="); attached && wanted[before] {
			selected, found = after, true
			continue
		}
		if wanted[arg] && index+1 < len(args) {
			selected, found = args[index+1], true
			index++
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(arg) > 2 && wanted[arg[:2]] {
			selected, found = arg[2:], true
		}
	}
	return selected, found
}

func flagValues(args []string, name string) []string {
	var values []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if arg == name && index+1 < len(args) {
			values = append(values, args[index+1])
			index++
			continue
		}
		if strings.HasPrefix(arg, name+"=") {
			values = append(values, strings.TrimPrefix(arg, name+"="))
			continue
		}
		if strings.HasPrefix(name, "-") && !strings.HasPrefix(name, "--") && strings.HasPrefix(arg, name) && len(arg) > len(name) {
			values = append(values, arg[len(name):])
		}
	}
	return values
}

func oneOwner(values []string) (string, bool) {
	var selected string
	for _, value := range values {
		owner := cleanOwner(value)
		if owner == "" || owner == "@me" {
			continue
		}
		if selected == "" {
			selected = owner
			continue
		}
		if !strings.EqualFold(selected, owner) {
			return "", false
		}
	}
	return selected, selected != ""
}

func cleanOwner(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "'\"")
	value = strings.Trim(value, "/")
	if value == "" || strings.ContainsAny(value, "/,{}") {
		return ""
	}
	return value
}

func hasHelpFlag(args []string) bool {
	for index, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--help" {
			return true
		}
		if arg == "-h" && (index == 0 || index == len(args)-1 || strings.HasPrefix(args[index+1], "-")) {
			return true
		}
	}
	return false
}
