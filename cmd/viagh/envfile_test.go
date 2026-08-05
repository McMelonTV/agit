package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnvironmentFileAutoLoad(t *testing.T) {
	dir := t.TempDir()
	viaghDir := filepath.Join(dir, "viagh")
	if err := os.MkdirAll(viaghDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(viaghDir, "viagh.env"), []byte("export VIAGH_APP_ID=4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	_, stderr, code := runMainSubprocess(t, nil, "token")
	if code == 0 {
		t.Fatal("viagh token unexpectedly succeeded without a private key")
	}
	if strings.Contains(stderr, "missing GitHub App ID") {
		t.Fatalf("VIAGH_APP_ID from the environment file was not loaded: %s", stderr)
	}
	if !strings.Contains(stderr, "missing GitHub App private key") {
		t.Fatalf("unexpected failure: %s", stderr)
	}
}
