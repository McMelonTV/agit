package wrapper

import (
	"os/exec"
	"strings"
)

func GitCommandIsAlias(realGit string, args, env []string) bool {
	command, commandIndex, ok := GitCommand(args)
	if !ok || strings.TrimSpace(realGit) == "" {
		return false
	}
	prefix := append([]string(nil), args[:commandIndex]...)
	if len(prefix) > 0 && prefix[len(prefix)-1] == "--" {
		prefix = prefix[:len(prefix)-1]
	}
	query := append(prefix, "config", "--get", "alias."+command)
	cmd := exec.Command(realGit, query...)
	cmd.Env = env
	cmd.Stderr = nil
	output, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(output)) != ""
}

// GitTopLevel reports the absolute working-tree root of the repository
// selected by args, using the same leading options as the wrapped command.
func GitTopLevel(realGit string, args, env []string) (string, bool) {
	_, commandIndex, ok := GitCommand(args)
	if !ok || strings.TrimSpace(realGit) == "" {
		return "", false
	}
	prefix := append([]string(nil), args[:commandIndex]...)
	if len(prefix) > 0 && prefix[len(prefix)-1] == "--" {
		prefix = prefix[:len(prefix)-1]
	}
	query := append(prefix, "rev-parse", "--show-toplevel")
	cmd := exec.Command(realGit, query...)
	cmd.Env = env
	cmd.Stderr = nil
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	dir := strings.TrimSpace(string(output))
	return dir, dir != ""
}

func commitCreatingCommand(command string) bool {
	switch command {
	case "am", "cherry-pick", "commit", "commit-tree", "merge", "notes", "pull", "rebase", "revert", "stash", "tag":
		return true
	default:
		return false
	}
}

func annotatedTag(args []string) bool {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "-a", "--annotate", "-s", "--sign", "-u", "--local-user", "-m", "--message", "-F", "--file":
			return true
		case "-l", "--list", "-d", "--delete", "-v", "--verify":
			return false
		}
		if len(arg) > 2 {
			switch arg[:2] {
			case "-u", "-m", "-F":
				return true
			case "-l":
				return false
			}
		}
	}
	return false
}

func stashCreatesCommit(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "list", "show", "drop", "pop", "apply", "branch", "clear":
			return false
		case "push", "save", "create", "store":
			return true
		default:
			return true
		}
	}
	return true
}

func notesCreateCommit(args []string) bool {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--ref":
			index++
			continue
		case strings.HasPrefix(arg, "--ref="):
			continue
		case arg == "--":
			return false
		case strings.HasPrefix(arg, "-"):
			continue
		}
		switch arg {
		case "list", "show", "get-ref":
			return false
		case "add", "append", "copy", "edit", "merge", "remove", "prune":
			return true
		default:
			// With no recognized subcommand, Git treats a positional value as
			// an object to list or rejects it. Neither path creates a notes
			// commit, so keep identity resolution lazy.
			return false
		}
	}
	return false
}

func hasAnyArg(args []string, values ...string) bool {
	for _, arg := range args {
		for _, value := range values {
			if arg == value {
				return true
			}
		}
	}
	return false
}

func GitUsesCommitMessageHook(args []string) bool {
	command, _, ok := GitCommand(args)
	if !ok {
		return false
	}
	switch command {
	case "am", "cherry-pick", "commit", "merge", "pull", "rebase", "revert":
		return true
	default:
		return false
	}
}

func AddCommitTreeTrailer(args []string, trailer string) []string {
	command, _, ok := GitCommand(args)
	trailer = strings.TrimSpace(trailer)
	if !ok || command != "commit-tree" || trailer == "" {
		return args
	}
	for index, arg := range args {
		switch {
		case (arg == "-m" || arg == "--message") && index+1 < len(args) && strings.TrimSpace(args[index+1]) == trailer:
			return args
		case strings.HasPrefix(arg, "-m") && len(arg) > 2 && strings.TrimSpace(arg[2:]) == trailer:
			return args
		case strings.HasPrefix(arg, "--message=") && strings.TrimSpace(strings.TrimPrefix(arg, "--message=")) == trailer:
			return args
		}
	}
	result := append([]string(nil), args...)
	return append(result, "-m", trailer)
}
