package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/McMelonTV/viagh/internal/config"
)

func unsetEnvVar(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
		}
	})
}

func TestEnvCommandFromProcessEnvironment(t *testing.T) {
	t.Setenv(config.SessionConfigEnv, "")
	t.Setenv("VIAGH_OWNER", "acme")
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "env", "VIAGH_OWNER"})
	})
	if code != 0 || stderr != "" || stdout != "VIAGH_OWNER=acme (env)\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestEnvCommandFromEnvFile(t *testing.T) {
	t.Setenv(config.SessionConfigEnv, "")
	dir := t.TempDir()
	path := filepath.Join(dir, "viagh.env")
	if err := os.WriteFile(path, []byte("export VIAGH_APP_ID=4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIAGH_CONFIG_FILE", path)
	unsetEnvVar(t, "VIAGH_APP_ID")
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "env", "VIAGH_APP_ID"})
	})
	if code != 0 || stderr != "" || stdout != "VIAGH_APP_ID=4242 (env_file)\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestEnvCommandNotSet(t *testing.T) {
	t.Setenv(config.SessionConfigEnv, "")
	unsetEnvVar(t, "VIAGH_UNRELATED_KEY")
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"viagh", "env", "VIAGH_UNRELATED_KEY"})
	})
	if code != 1 || stdout != "" || !strings.Contains(stderr, "VIAGH_UNRELATED_KEY is not set") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestEnvCommandRequiresExactlyOneKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdout capture is Unix-specific")
	}
	t.Setenv(config.SessionConfigEnv, "")
	for _, args := range [][]string{
		{"viagh", "env"},
		{"viagh", "env", "VIAGH_OWNER", "VIAGH_APP_ID"},
	} {
		stdout, stderr, code := captureProcessOutput(t, func() int {
			return run(args)
		})
		if code != 1 || stdout != "" || !strings.Contains(stderr, "env requires exactly one variable name") {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}
