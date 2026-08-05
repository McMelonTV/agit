package wrapper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfiguredBareBinarySkipsCurrentExecutableShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test is Unix-specific")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	realDir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(shimDir, "gh")); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(realDir, "gh")
	if err := os.WriteFile(real, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+realDir)
	got, err := validateConfiguredBinary("gh", "gh")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(real)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
