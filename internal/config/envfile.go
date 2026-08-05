package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	envFileName       = "viagh.env"
	configFileEnv     = "VIAGH_CONFIG_FILE"
	configFileDirName = "viagh"
)

// Env source labels reported by LookupEnvSource.
const (
	EnvSourceProcess = "env"
	EnvSourceFile    = "env_file"
)

// envFileKeys records variables applied from the environment file so callers
// can distinguish their origin from the process environment.
var envFileKeys = make(map[string]bool)

// LoadEnvFile applies variables from the viagh environment file to the
// process environment. An explicit VIAGH_CONFIG_FILE path takes precedence;
// otherwise $XDG_CONFIG_HOME/viagh/viagh.env is used on Unix and
// %AppData%\viagh\viagh.env on Windows. A missing file is not an error.
//
// Variables already present in the process environment take precedence over
// the file, and empty values are ignored. Values are literal: no shell
// expansion, command substitution, or variable interpolation is performed.
func LoadEnvFile() error {
	envFileKeys = make(map[string]bool)
	path, err := EnvFilePath()
	if err != nil || path == "" {
		return nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open environment file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("parse %s: line %d: expected KEY=VALUE", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		if !validEnvKey(key) {
			return fmt.Errorf("parse %s: line %d: invalid variable name %q", path, lineNumber, key)
		}
		value = unquoteEnvValue(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
		envFileKeys[key] = true
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

// LookupEnvSource returns the value of an environment variable and the
// source that set it: EnvSourceProcess ("env") for the process environment or
// EnvSourceFile ("env_file") when LoadEnvFile applied it from the environment
// file. ok is false when the variable is not set.
func LookupEnvSource(key string) (value, source string, ok bool) {
	value, ok = os.LookupEnv(key)
	if !ok {
		return "", "", false
	}
	if envFileKeys[key] {
		return value, EnvSourceFile, true
	}
	return value, EnvSourceProcess, true
}

// EnvFilePath returns the path of the viagh environment file, or "" when no
// default location exists.
func EnvFilePath() (string, error) {
	if value := os.Getenv(configFileEnv); value != "" {
		return value, nil
	}
	var base string
	if runtime.GOOS == "windows" {
		base = os.Getenv("AppData")
		if base == "" {
			return "", nil
		}
	} else {
		base = os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return "", nil
			}
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, configFileDirName, envFileName), nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func unquoteEnvValue(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}
