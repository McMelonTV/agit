package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func writeEnvFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	viaghDir := filepath.Join(dir, "viagh")
	if err := os.MkdirAll(viaghDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(viaghDir, "viagh.env")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	t.Setenv(configFileEnv, "")
	return path
}

func TestLoadEnvFileMissingIsNotAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", t.TempDir())
	} else {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
	t.Setenv(configFileEnv, "")
	if err := LoadEnvFile(); err != nil {
		t.Fatalf("LoadEnvFile() error for missing file: %v", err)
	}
}

func TestLoadEnvFileAppliesExports(t *testing.T) {
	writeEnvFile(t, `
# comment line
export VIAGH_APP_ID=4242

VIAGH_OWNER=acme
VIAGH_GIT_NAME='Release Agent'
VIAGH_GIT_EMAIL="release-agent@example.com"
`)
	unsetEnvForTest(t, "VIAGH_APP_ID")
	unsetEnvForTest(t, "VIAGH_OWNER")
	unsetEnvForTest(t, "VIAGH_GIT_NAME")
	unsetEnvForTest(t, "VIAGH_GIT_EMAIL")
	if err := LoadEnvFile(); err != nil {
		t.Fatalf("LoadEnvFile() error: %v", err)
	}
	for key, want := range map[string]string{
		"VIAGH_APP_ID":    "4242",
		"VIAGH_OWNER":     "acme",
		"VIAGH_GIT_NAME":  "Release Agent",
		"VIAGH_GIT_EMAIL": "release-agent@example.com",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadEnvFileExistingEnvironmentWins(t *testing.T) {
	writeEnvFile(t, "export VIAGH_APP_ID=4242\n")
	t.Setenv("VIAGH_APP_ID", "99")
	if err := LoadEnvFile(); err != nil {
		t.Fatalf("LoadEnvFile() error: %v", err)
	}
	if got := os.Getenv("VIAGH_APP_ID"); got != "99" {
		t.Fatalf("VIAGH_APP_ID = %q, want existing value 99", got)
	}
}

func TestLoadEnvFileIgnoresEmptyValues(t *testing.T) {
	writeEnvFile(t, "export VIAGH_APP_ID=\n")
	unsetEnvForTest(t, "VIAGH_APP_ID")
	if err := LoadEnvFile(); err != nil {
		t.Fatalf("LoadEnvFile() error: %v", err)
	}
	if _, exists := os.LookupEnv("VIAGH_APP_ID"); exists {
		t.Fatal("VIAGH_APP_ID was set from an empty value")
	}
}

func TestLoadEnvFileParseError(t *testing.T) {
	path := writeEnvFile(t, "export VIAGH_APP_ID=4242\nthis line has no equals sign\n")
	if err := LoadEnvFile(); err == nil {
		t.Fatal("LoadEnvFile() succeeded for a malformed line")
	} else if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("LoadEnvFile() error = %v, want path and line number", err)
	}
}

func TestLoadEnvFileInvalidVariableName(t *testing.T) {
	writeEnvFile(t, "1VIAGH_APP_ID=4242\n")
	if err := LoadEnvFile(); err == nil {
		t.Fatal("LoadEnvFile() succeeded for an invalid variable name")
	} else if !strings.Contains(err.Error(), "invalid variable name") {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}
}

func TestLoadEnvFileOverridePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.env")
	if err := os.WriteFile(path, []byte("export VIAGH_OWNER=beta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configFileEnv, path)
	unsetEnvForTest(t, "VIAGH_OWNER")
	if err := LoadEnvFile(); err != nil {
		t.Fatalf("LoadEnvFile() error: %v", err)
	}
	if got := os.Getenv("VIAGH_OWNER"); got != "beta" {
		t.Fatalf("VIAGH_OWNER = %q, want beta", got)
	}
}
