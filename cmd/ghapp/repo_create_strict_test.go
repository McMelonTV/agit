package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/McMelonTV/agit/internal/config"
)

func TestRepoCreateUnqualifiedNameRequiresConfiguredOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-specific")
	}
	marker := filepath.Join(t.TempDir(), "calls")
	t.Setenv("CALL_MARKER", marker)
	fakeGH := writeExecutable(t, `#!/bin/sh
printf 'gh\n' >> "$CALL_MARKER"
exit 97
`)

	for name, argv := range map[string][]string{
		"wrapper": {"ghapp", "--real-gh", fakeGH, "gh", "repo", "create", "widgets", "--private"},
		"shim":    {fakeGH, "repo", "create", "widgets", "--private"},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := captureProcessOutput(t, func() int { return run(argv) })
			if code != 1 || stdout != "" || !strings.Contains(stderr, "unqualified gh repo create target requires an explicit owner") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("underlying gh was invoked: %s", marker)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestRepoCreateConfiguredOwnerPassesValidation(t *testing.T) {
	cfg := config.Config{Owner: "acme"}
	if err := validateRepoCreateOwner(cfg, "gh", []string{"repo", "create", "widgets"}); err != nil {
		t.Fatal(err)
	}
	if err := validateRepoCreateOwner(config.Config{}, "gh", []string{"repo", "create", "acme/widgets"}); err != nil {
		t.Fatal(err)
	}
}
