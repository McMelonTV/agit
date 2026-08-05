//go:build !windows

package wrapper

import "testing"

func TestRunMapsSignalExitCode(t *testing.T) {
	code := Run("/bin/sh", []string{"-c", "kill -TERM $$"}, nil)
	if code != 143 {
		t.Fatalf("exit code = %d, want 143", code)
	}
}
