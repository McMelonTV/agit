package wrapper

import (
	"strconv"
	"strings"
	"testing"
)

func TestApplyGitAuthPreservesExistingConfigAndScopesHelper(t *testing.T) {
	env := []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.autocrlf",
		"GIT_CONFIG_VALUE_0=false",
		"GHAPP_RUNTIME_TOKEN=stale",
		"GIT_ASKPASS=/tmp/old",
	}
	got, err := ApplyGitAuth(env, "/tmp/ghapp", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	countText, ok := LookupEnv(got, "GIT_CONFIG_COUNT")
	if !ok {
		t.Fatal("missing GIT_CONFIG_COUNT")
	}
	count, err := strconv.Atoi(countText)
	if err != nil {
		t.Fatal(err)
	}
	if count != 15 {
		t.Fatalf("GIT_CONFIG_COUNT = %d, want 15", count)
	}
	if _, ok := LookupEnv(got, "GHAPP_RUNTIME_TOKEN"); ok {
		t.Fatal("stale runtime token was retained")
	}
	if value, ok := LookupEnv(got, "GIT_ASKPASS"); !ok || value != "/tmp/old" {
		t.Fatal("unrelated askpass configuration was not preserved")
	}
	var helper string
	for index := 0; index < count; index++ {
		key, _ := LookupEnv(got, "GIT_CONFIG_KEY_"+strconv.Itoa(index))
		value, _ := LookupEnv(got, "GIT_CONFIG_VALUE_"+strconv.Itoa(index))
		if key == "credential.https://github.com.helper" && strings.HasPrefix(value, "!") {
			helper = value
		}
		if key == "credential.helper" {
			t.Fatal("global credential helper was modified")
		}
	}
	if helper == "" || !strings.Contains(helper, "credential-helper") {
		t.Fatalf("missing scoped helper: %q", helper)
	}
}

func TestApplyGitAuthIncludesSecureRemoteRewrites(t *testing.T) {
	env, err := ApplyGitAuth(nil, "/tmp/ghapp", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	countText, _ := LookupEnv(env, "GIT_CONFIG_COUNT")
	count, _ := strconv.Atoi(countText)
	wanted := map[string]bool{
		"http://github.com/":                false,
		"ssh://git@github.com:22/":          false,
		"ssh://git@ssh.github.com:443/":     false,
		"git+ssh://git@ssh.github.com:443/": false,
	}
	for index := 0; index < count; index++ {
		key, _ := LookupEnv(env, "GIT_CONFIG_KEY_"+strconv.Itoa(index))
		value, _ := LookupEnv(env, "GIT_CONFIG_VALUE_"+strconv.Itoa(index))
		if strings.HasPrefix(key, "url.https://github.com/.insteadof") {
			if _, ok := wanted[value]; ok {
				wanted[value] = true
			}
		}
	}
	for value, found := range wanted {
		if !found {
			t.Fatalf("missing rewrite for %q", value)
		}
	}
}

func TestSanitizeEnvRemovesPrivateKeyMaterial(t *testing.T) {
	got := SanitizeEnv([]string{
		"GHAPP_PRIVATE_KEY=/key",
		"GHAPP_PRIVATE_KEY_PEM=secret",
		"GHAPP_PRIVATE_KEY_BASE64=secret",
		"GITHUB_APP_PRIVATE_KEY=secret",
		"GIT_ASKPASS=/tmp/prompt",
		"SSH_ASKPASS=/tmp/ssh-prompt",
		"SAFE=value",
	})
	if value, ok := LookupEnv(got, "SAFE"); !ok || value != "value" {
		t.Fatal("safe environment entry was not preserved")
	}
	for key, want := range map[string]string{"GIT_ASKPASS": "/tmp/prompt", "SSH_ASKPASS": "/tmp/ssh-prompt"} {
		if value, ok := LookupEnv(got, key); !ok || value != want {
			t.Fatalf("%s was not preserved", key)
		}
	}
	for _, key := range []string{"GHAPP_PRIVATE_KEY", "GHAPP_PRIVATE_KEY_PEM", "GHAPP_PRIVATE_KEY_BASE64", "GITHUB_APP_PRIVATE_KEY"} {
		if _, ok := LookupEnv(got, key); ok {
			t.Fatalf("%s was not removed", key)
		}
	}
}

func TestStripGitAuthRestoresInheritedProcessConfig(t *testing.T) {
	env := []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.autocrlf",
		"GIT_CONFIG_VALUE_0=false",
	}
	wrapped, err := ApplyGitAuth(env, "/tmp/ghapp", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	stripped := StripGitAuth(wrapped)
	if count, _ := LookupEnv(stripped, "GIT_CONFIG_COUNT"); count != "1" {
		t.Fatalf("count = %q", count)
	}
	if key, _ := LookupEnv(stripped, "GIT_CONFIG_KEY_0"); key != "core.autocrlf" {
		t.Fatalf("inherited key = %q", key)
	}
	if _, ok := LookupEnv(stripped, "GIT_CONFIG_KEY_1"); ok {
		t.Fatal("ghapp Git configuration was retained")
	}
}

func TestRemoveActiveAppAuthPreservesUserOverride(t *testing.T) {
	env := ApplyGHAuth([]string{"SAFE=value"}, "github.com", true, "app-token")
	env = SetEnv(env, "GH_TOKEN", "user-token")
	cleaned := RemoveActiveAppAuth(env)
	if value, ok := LookupEnv(cleaned, "GH_TOKEN"); !ok || value != "user-token" {
		t.Fatalf("user override was not preserved: %q, %v", value, ok)
	}
	if _, ok := LookupEnv(cleaned, "GITHUB_TOKEN"); ok {
		t.Fatal("injected fallback app token was retained")
	}
	if _, ok := LookupEnv(cleaned, activeTokenHashEnv); ok {
		t.Fatal("active token marker was retained")
	}
}

func TestRemoveActiveAppAuthRemovesInjectedTokens(t *testing.T) {
	env := ApplyGHAuth(nil, "github.com", true, "app-token")
	cleaned := RemoveActiveAppAuth(env)
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", activeTokenHashEnv} {
		if _, ok := LookupEnv(cleaned, key); ok {
			t.Fatalf("%s was retained", key)
		}
	}
}

func TestClearGitAuthTrackingDoesNotAlterUserGitConfig(t *testing.T) {
	env := []string{
		"GHAPP_GIT_CONFIG_START=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.autocrlf",
		"GIT_CONFIG_VALUE_0=false",
	}
	cleaned := ClearGitAuthTracking(env)
	if _, ok := LookupEnv(cleaned, "GHAPP_GIT_CONFIG_START"); ok {
		t.Fatal("untrusted tracking marker was retained")
	}
	if value, _ := LookupEnv(cleaned, "GIT_CONFIG_KEY_0"); value != "core.autocrlf" {
		t.Fatalf("user Git config was altered: %q", value)
	}
}
