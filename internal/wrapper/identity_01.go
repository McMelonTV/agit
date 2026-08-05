package wrapper

import (
	"fmt"
	"strings"
)

const coauthorTrailerEnv = "VIAGH_COAUTHOR_TRAILER"

const opencodeTrailerEnv = "VIAGH_OPENCODE_TRAILER"

type GitIdentity struct {
	Name  string
	Email string
}

func (i GitIdentity) Valid() bool {
	name := strings.TrimSpace(i.Name)
	email := strings.TrimSpace(i.Email)
	return name != "" && email != "" &&
		!strings.ContainsAny(name, "\x00\r\n<>") &&
		!strings.ContainsAny(email, "\x00\r\n<> \t")
}

func (i GitIdentity) Trailer() string {
	return "Co-authored-by: " + strings.TrimSpace(i.Name) + " <" + strings.TrimSpace(i.Email) + ">"
}

func ApplyGitIdentity(env []string, identity GitIdentity) ([]string, error) {
	if !identity.Valid() {
		return nil, fmt.Errorf("Git identity requires both name and email")
	}
	name := strings.TrimSpace(identity.Name)
	email := strings.TrimSpace(identity.Email)
	for key, value := range map[string]string{
		"GIT_AUTHOR_NAME":     name,
		"GIT_AUTHOR_EMAIL":    email,
		"GIT_COMMITTER_NAME":  name,
		"GIT_COMMITTER_EMAIL": email,
	} {
		env = SetEnv(env, key, value)
	}
	var err error
	env, err = AppendGitConfig(env, "user.name", name)
	if err != nil {
		return nil, err
	}
	env, err = AppendGitConfig(env, "user.email", email)
	if err != nil {
		return nil, err
	}
	return env, nil
}

func GitCommand(args []string) (string, int, bool) {
	index := GitCommandIndex(args)
	if index < 0 || index >= len(args) {
		return "", -1, false
	}
	return args[index], index, true
}

func GitCommandIndex(args []string) int {
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

func GitMayCreateCommit(args []string) bool {
	command, index, ok := GitCommand(args)
	if !ok {
		return false
	}
	rest := args[index+1:]
	switch command {
	case "tag":
		return annotatedTag(rest)
	case "stash":
		return stashCreatesCommit(rest)
	case "notes":
		return notesCreateCommit(rest)
	case "am", "cherry-pick", "merge", "rebase", "revert":
		return !hasAnyArg(rest, "--abort", "--quit", "--show-current-patch")
	}
	return commitCreatingCommand(command)
}

// GitCommandIsAlias reports whether the command selected by args is defined as
// a Git alias in the effective configuration for this invocation. Aliases are
// treated conservatively as commit-producing by the caller because an alias
// may expand to a built-in command or an arbitrary shell command.
