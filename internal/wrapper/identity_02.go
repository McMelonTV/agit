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

	tempDir, err := os.MkdirTemp("", "viagh-hooks-")
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
	if originalExists {
		script.WriteString(shellQuote(hookExecutable(original)))
		script.WriteString(" \"$@\" || exit $?\n")
	}
	script.WriteString("exec ")
	script.WriteString(shellQuote(hookExecutable(executable)))
	script.WriteString(" commit-message-hook \"$1\"\n")
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
