package wrapper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const gitConfigStartEnv = "VIAGH_GIT_CONFIG_START"

const activeTokenHashEnv = "VIAGH_ACTIVE_TOKEN_SHA256"

var sensitiveEnvironment = []string{
	"VIAGH_PRIVATE_KEY",
	"VIAGH_PRIVATE_KEY_PEM",
	"VIAGH_PRIVATE_KEY_BASE64",
	"GITHUB_APP_PRIVATE_KEY",
	"VIAGH_RUNTIME_TOKEN",
	"VIAGH_RUNTIME_REPOSITORY",
	"VIAGH_ASKPASS",
	coauthorTrailerEnv,
	opencodeTrailerEnv,
	activeTokenHashEnv,
}

func SanitizeEnv(env []string) []string {
	result := append([]string(nil), env...)
	for _, key := range sensitiveEnvironment {
		result = UnsetEnv(result, key)
	}
	return result
}

func ClearGitAuthTracking(env []string) []string {
	return UnsetEnv(env, gitConfigStartEnv)
}

func ApplyGitAuth(env []string, executable, host string) ([]string, error) {
	if _, exists := LookupEnv(env, gitConfigStartEnv); !exists {
		count, err := gitConfigCount(env)
		if err != nil {
			return nil, err
		}
		env = SetEnv(env, gitConfigStartEnv, strconv.Itoa(count))
	}
	env = UnsetEnv(env, "VIAGH_RUNTIME_TOKEN")
	env = UnsetEnv(env, "VIAGH_RUNTIME_REPOSITORY")
	env = UnsetEnv(env, "VIAGH_ASKPASS")

	helperExecutable := executable
	if runtime.GOOS == "windows" {
		// Git for Windows executes shell credential helpers through its POSIX
		// shell, where native backslashes are escape characters.
		helperExecutable = filepath.ToSlash(helperExecutable)
	}
	helper := "!" + shellQuote(helperExecutable) + " credential-helper"
	pairs := [][2]string{
		{"credential.https://" + host + ".helper", ""},
		{"credential.https://" + host + ".helper", helper},
		{"credential.https://" + host + ".username", "x-access-token"},
		{"credential.https://" + host + ".useHttpPath", "true"},
		{"http.https://" + host + "/.extraheader", ""},
		{"url.https://" + host + "/.insteadof", "git@" + host + ":"},
		{"url.https://" + host + "/.insteadof", "ssh://git@" + host + "/"},
		{"url.https://" + host + "/.insteadof", "ssh://git@" + host + ":22/"},
		{"url.https://" + host + "/.insteadof", "git+ssh://git@" + host + "/"},
		{"url.https://" + host + "/.insteadof", "git+ssh://git@" + host + ":22/"},
		{"url.https://" + host + "/.insteadof", "git://" + host + "/"},
		{"url.https://" + host + "/.insteadof", "http://" + host + "/"},
	}
	if strings.EqualFold(host, "github.com") {
		pairs = append(pairs,
			[2]string{"url.https://github.com/.insteadof", "ssh://git@ssh.github.com:443/"},
			[2]string{"url.https://github.com/.insteadof", "git+ssh://git@ssh.github.com:443/"},
		)
	}
	var err error
	for _, pair := range pairs {
		env, err = AppendGitConfig(env, pair[0], pair[1])
		if err != nil {
			return nil, err
		}
	}
	return env, nil
}

func ApplyGHAuth(env []string, host string, cloudHost bool, token string) []string {
	env = ClearGHAuth(env)
	env = SetEnv(env, "GH_HOST", host)
	if cloudHost {
		env = SetEnv(env, "GH_TOKEN", token)
		env = SetEnv(env, "GITHUB_TOKEN", token)
	} else {
		env = SetEnv(env, "GH_ENTERPRISE_TOKEN", token)
		env = SetEnv(env, "GITHUB_ENTERPRISE_TOKEN", token)
	}
	env = SetEnv(env, activeTokenHashEnv, tokenHash(token))
	return env
}

func ClearGHAuth(env []string) []string {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		env = UnsetEnv(env, key)
	}
	env = UnsetEnv(env, activeTokenHashEnv)
	return env
}

// RemoveActiveAppAuth removes only token values that were injected by the
// current viagh process. A nested command may deliberately replace GH_TOKEN
// with a user token for another host; that replacement is preserved.
func RemoveActiveAppAuth(env []string) []string {
	expected, ok := LookupEnv(env, activeTokenHashEnv)
	result := append([]string(nil), env...)
	if ok && expected != "" {
		for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
			if value, found := LookupEnv(result, key); found && tokenHash(value) == expected {
				result = UnsetEnv(result, key)
			}
		}
	}
	return UnsetEnv(result, activeTokenHashEnv)
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func AppendGitConfig(env []string, key, value string) ([]string, error) {
	count, err := gitConfigCount(env)
	if err != nil {
		return nil, err
	}
	env = SetEnv(env, fmt.Sprintf("GIT_CONFIG_KEY_%d", count), key)
	env = SetEnv(env, fmt.Sprintf("GIT_CONFIG_VALUE_%d", count), value)
	env = SetEnv(env, "GIT_CONFIG_COUNT", strconv.Itoa(count+1))
	return env, nil
}

func StripGitAuth(env []string) []string {
	startText, ok := LookupEnv(env, gitConfigStartEnv)
	if !ok {
		return env
	}
	start, err := strconv.Atoi(startText)
	if err != nil || start < 0 {
		return UnsetEnv(env, gitConfigStartEnv)
	}
	count, err := gitConfigCount(env)
	if err != nil || start > count {
		return UnsetEnv(env, gitConfigStartEnv)
	}
	result := append([]string(nil), env...)
	for index := start; index < count; index++ {
		result = UnsetEnv(result, fmt.Sprintf("GIT_CONFIG_KEY_%d", index))
		result = UnsetEnv(result, fmt.Sprintf("GIT_CONFIG_VALUE_%d", index))
	}
	result = SetEnv(result, "GIT_CONFIG_COUNT", strconv.Itoa(start))
	result = UnsetEnv(result, gitConfigStartEnv)
	return result
}

func gitConfigCount(env []string) (int, error) {
	countText, _ := LookupEnv(env, "GIT_CONFIG_COUNT")
	if countText == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(countText)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("invalid inherited GIT_CONFIG_COUNT %q", countText)
	}
	return count, nil
}

func SetEnv(env []string, key, value string) []string {
	entry := key + "=" + value
	result := append([]string(nil), env...)
	for index, existing := range result {
		name, _, _ := strings.Cut(existing, "=")
		if envKeyEqual(name, key) {
			result[index] = entry
			return result
		}
	}
	return append(result, entry)
}

func UnsetEnv(env []string, key string) []string {
	result := make([]string, 0, len(env))
	for _, existing := range env {
		name, _, _ := strings.Cut(existing, "=")
		if !envKeyEqual(name, key) {
			result = append(result, existing)
		}
	}
	return result
}

func LookupEnv(env []string, key string) (string, bool) {
	for _, existing := range env {
		name, value, found := strings.Cut(existing, "=")
		if found && envKeyEqual(name, key) {
			return value, true
		}
	}
	return "", false
}

func envKeyEqual(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func CleanExecutablePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}
