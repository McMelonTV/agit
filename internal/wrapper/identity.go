package wrapper

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const coauthorTrailerEnv = "GHAPP_COAUTHOR_TRAILER"

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
	if !ok || command != "commit-tree" || strings.TrimSpace(trailer) == "" {
		return args
	}
	result := append([]string(nil), args...)
	return append(result, "-m", trailer)
}

func PrepareCommitHooks(realGit, executable string, args, env []string, trailer string) (string, func(), error) {
	if strings.TrimSpace(trailer) == "" {
		return "", func() {}, nil
	}
	_, commandIndex, ok := GitCommand(args)
	if !ok {
		return "", func() {}, nil
	}
	prefix := append([]string(nil), args[:commandIndex]...)
	gitArgs := append(prefix, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	cmd := exec.Command(realGit, gitArgs...)
	cmd.Env = env
	cmd.Stderr = nil
	output, err := cmd.Output()
	if err != nil {
		// Let the real Git command report repository or argument errors.
		return "", func() {}, nil
	}
	originalDir := strings.TrimSpace(string(output))
	if originalDir == "" {
		return "", func() {}, nil
	}

	tempDir, err := os.MkdirTemp("", "ghapp-hooks-")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary Git hooks directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	if err := mirrorHookDelegates(originalDir, tempDir); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := writePrepareCommitMsgHook(tempDir, originalDir, executable); err != nil {
		cleanup()
		return "", nil, err
	}
	return tempDir, cleanup, nil
}

func mirrorHookDelegates(originalDir, tempDir string) error {
	entries, err := os.ReadDir(originalDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read existing Git hooks directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "prepare-commit-msg" {
			continue
		}
		original := filepath.Join(originalDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect Git hook %s: %w", original, err)
		}
		if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
			continue
		}
		if err := writeDelegateHook(filepath.Join(tempDir, entry.Name()), original); err != nil {
			return err
		}
	}
	return nil
}

func writePrepareCommitMsgHook(tempDir, originalDir, executable string) error {
	original := filepath.Join(originalDir, "prepare-commit-msg")
	originalExists := executableFile(original)
	var script bytes.Buffer
	script.WriteString("#!/bin/sh\n")
	script.WriteString(shellQuote(hookExecutable(executable)))
	script.WriteString(" commit-message-hook \"$1\" || exit $?\n")
	if originalExists {
		script.WriteString("exec ")
		script.WriteString(shellQuote(hookExecutable(original)))
		script.WriteString(" \"$@\"\n")
	}
	return writeHook(filepath.Join(tempDir, "prepare-commit-msg"), script.Bytes())
}

func writeDelegateHook(path, original string) error {
	script := []byte("#!/bin/sh\nexec " + shellQuote(hookExecutable(original)) + " \"$@\"\n")
	return writeHook(path, script)
}

func writeHook(path string, contents []byte) error {
	if err := os.WriteFile(path, contents, 0o700); err != nil {
		return fmt.Errorf("write Git hook %s: %w", path, err)
	}
	return nil
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

func hookExecutable(path string) string {
	if runtime.GOOS == "windows" {
		return filepath.ToSlash(path)
	}
	return path
}

func SetCoauthorTrailer(env []string, trailer string) []string {
	if strings.TrimSpace(trailer) == "" {
		return UnsetEnv(env, coauthorTrailerEnv)
	}
	return SetEnv(env, coauthorTrailerEnv, trailer)
}

func AppendCoauthorTrailer(message []byte, trailer string) []byte {
	trailer = strings.TrimSpace(trailer)
	if trailer == "" {
		return message
	}
	normalized := strings.ReplaceAll(string(message), "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), trailer) {
			return message
		}
	}
	trimmed := strings.TrimRight(normalized, "\n")
	if trimmed != "" {
		trimmed += "\n\n"
	}
	trimmed += trailer + "\n"
	return []byte(trimmed)
}

func CoauthorTrailerFromEnv(env []string) string {
	value, _ := LookupEnv(env, coauthorTrailerEnv)
	return value
}
